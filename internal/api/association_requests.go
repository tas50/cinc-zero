package api

import (
	"encoding/json"
	"net/http"
	"slices"

	"github.com/tas50/cinc-zero/internal/store"
)

// Association requests are the invite flow: an org admin invites a global user,
// and the user accepts or rejects. Pending invites live in each org's
// "association_requests" collection keyed by a deterministic id; accepting an
// invite associates the user (see association.go).

const assocReqColl = "association_requests"

func inviteID(user, org string) string { return user + "-" + org }

// writeStringError writes an error body as a JSON object with a string "error"
// field — the shape Chef Infra Server uses for the association / invitation
// endpoints, distinct from the {"error":[...]} array used elsewhere.
func writeStringError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (a *API) registerAssociationRequestRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /organizations/{org}/association_requests", a.listOrgInvites)
	mux.HandleFunc("POST /organizations/{org}/association_requests", a.createInvite)
	mux.HandleFunc("DELETE /organizations/{org}/association_requests/{id}", a.rescindInvite)

	mux.HandleFunc("GET /users/{user}/association_requests", a.listUserInvites)
	mux.HandleFunc("GET /users/{user}/association_requests/count", a.countUserInvites)
	mux.HandleFunc("PUT /users/{user}/association_requests/{id}", a.respondInvite)
}

func (a *API) listOrgInvites(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	out := make([]map[string]any, 0)
	for _, id := range org.Keys(assocReqColl) {
		raw, _ := org.Get(assocReqColl, id)
		var inv map[string]any
		json.Unmarshal(raw, &inv)
		out = append(out, map[string]any{"id": id, "username": inv["username"]})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) createInvite(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	user, _ := body["user"].(string)
	if user == "" {
		user, _ = body["username"].(string)
	}
	if user == "" {
		writeError(w, http.StatusBadRequest, "Field 'user' missing")
		return
	}
	if _, ok := a.store.Global().Get("users", user); !ok {
		writeError(w, http.StatusNotFound, "Cannot find user "+user)
		return
	}
	if _, ok := org.Get(assocColl, user); ok {
		writeStringError(w, http.StatusConflict, "The association already exists.")
		return
	}
	id := inviteID(user, org.Name())
	if _, ok := org.Get(assocReqColl, id); ok {
		writeStringError(w, http.StatusConflict, "The invitation already exists.")
		return
	}
	// Record who issued the invite so acceptance can verify they still have the
	// authority to do so.
	inviter := ""
	if actor, ok := actorFromContext(r.Context()); ok {
		inviter = actor.Name
	}
	org.Put(assocReqColl, id, mustEncode(map[string]any{
		"id": id, "username": user, "orgname": org.Name(), "inviter": inviter,
	}))
	writeJSON(w, http.StatusCreated, map[string]any{
		"uri":               requestBaseURL(r) + "/organizations/" + org.Name() + "/association_requests/" + id,
		"id":                id,
		"organization_user": map[string]any{"username": user},
	})
}

func (a *API) rescindInvite(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	raw, ok := org.Delete(assocReqColl, r.PathValue("id"))
	if !ok {
		writeStringError(w, http.StatusNotFound, "Cannot find association request: "+r.PathValue("id"))
		return
	}
	writeRaw(w, http.StatusOK, raw)
}

func (a *API) listUserInvites(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.userInvites(r.PathValue("user")))
}

func (a *API) countUserInvites(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"value": len(a.userInvites(r.PathValue("user")))})
}

// userInvites collects the pending invites for a user across all organizations.
func (a *API) userInvites(user string) []map[string]any {
	out := make([]map[string]any, 0)
	for _, name := range a.store.ListOrgs() {
		org, ok := a.store.Org(name)
		if !ok {
			continue
		}
		if raw, ok := org.Get(assocReqColl, inviteID(user, name)); ok {
			var inv map[string]any
			json.Unmarshal(raw, &inv)
			out = append(out, map[string]any{"id": inviteID(user, name), "orgname": name})
		}
	}
	return out
}

// respondInvite accepts or rejects an invitation. Only the invited user may
// respond. Accepting associates the user with the org and adds them to its
// "users" group; either response consumes the invite. An invite issued by
// someone who has since lost the authority to invite can no longer be accepted.
func (a *API) respondInvite(w http.ResponseWriter, r *http.Request) {
	user, id := r.PathValue("user"), r.PathValue("id")

	// Only the invitee may respond to their own invitation; a third party (org
	// admin or anyone else) is forbidden. With no actor the endpoint stays open.
	if actor, ok := actorFromContext(r.Context()); ok && actor.Name != user {
		writeStringError(w, http.StatusForbidden, "You are not allowed to take this action.")
		return
	}

	org, ok := a.findInvite(user, id)
	if !ok {
		writeStringError(w, http.StatusNotFound, "Cannot find association request: "+id)
		return
	}
	var body struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Response != "accept" && body.Response != "reject" {
		writeError(w, http.StatusBadRequest, "Param response must be either 'accept' or 'reject'")
		return
	}

	if body.Response == "accept" {
		var inv struct {
			Inviter string `json:"inviter"`
		}
		if raw, ok := org.Get(assocReqColl, id); ok {
			json.Unmarshal(raw, &inv)
		}
		if !a.inviterAuthorized(org, inv.Inviter) {
			writeStringError(w, http.StatusForbidden, "The user who issued this invitation can no longer do so.")
			return
		}
	}

	org.Delete(assocReqColl, id)
	if body.Response == "accept" {
		org.Put(assocColl, user, mustEncode(map[string]any{"username": user}))
		addUserToOrgGroup(org, "users", user)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"organization": map[string]any{"name": org.Name()},
	})
}

// inviterAuthorized reports whether the recorded inviter may still issue
// invitations for the org: a global superuser always may; otherwise they must
// still exist, still be associated with the org, and still belong to its
// "admins" group. An empty inviter (created without an authenticated actor) is
// treated as authorized.
func (a *API) inviterAuthorized(org *store.Org, inviter string) bool {
	if inviter == "" {
		return true
	}
	uraw, ok := a.store.Global().Get("users", inviter)
	if !ok {
		return false
	}
	var u map[string]any
	json.Unmarshal(uraw, &u)
	if admin, _ := u["admin"].(bool); admin {
		return true
	}
	if _, ok := org.Get(assocColl, inviter); !ok {
		return false
	}
	if raw, ok := org.Get("groups", "admins"); ok {
		var g map[string]any
		if json.Unmarshal(raw, &g) == nil && slices.Contains(anyStrings(g["users"]), inviter) {
			return true
		}
	}
	return false
}

// addUserToOrgGroup adds user to the named org group's user list (creating the
// group if absent), so authorization membership reflects the new member.
func addUserToOrgGroup(org *store.Org, group, user string) {
	var users, clients, groups []string
	if raw, ok := org.Get("groups", group); ok {
		var g map[string]any
		if json.Unmarshal(raw, &g) == nil {
			users, clients, groups = groupMembers(g)
		}
	}
	if slices.Contains(users, user) {
		return
	}
	users = append(users, user)
	org.Put("groups", group, mustEncode(groupDoc(group, users, clients, groups)))
}

// findInvite locates the org holding the given invitation id for the user.
func (a *API) findInvite(user, id string) (*store.Org, bool) {
	for _, name := range a.store.ListOrgs() {
		org, ok := a.store.Org(name)
		if !ok {
			continue
		}
		if raw, ok := org.Get(assocReqColl, id); ok {
			var inv struct {
				Username string `json:"username"`
			}
			json.Unmarshal(raw, &inv)
			if inv.Username == user {
				return org, true
			}
		}
	}
	return nil, false
}
