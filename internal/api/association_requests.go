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
		writeError(w, http.StatusConflict, "The association already exists.")
		return
	}
	id := inviteID(user, org.Name())
	if _, ok := org.Get(assocReqColl, id); ok {
		writeError(w, http.StatusConflict, "The invitation already exists.")
		return
	}
	org.Put(assocReqColl, id, mustEncode(map[string]any{
		"id": id, "username": user, "orgname": org.Name(),
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
		writeError(w, http.StatusNotFound, "Cannot find association request: "+r.PathValue("id"))
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

// respondInvite accepts or rejects an invitation. Accepting associates the user
// with the org and adds them to its "users" group; either response clears the
// invite. A response other than accept/reject is rejected and leaves the invite
// intact.
func (a *API) respondInvite(w http.ResponseWriter, r *http.Request) {
	user, id := r.PathValue("user"), r.PathValue("id")
	org, ok := a.findInvite(user, id)
	if !ok {
		writeError(w, http.StatusNotFound, "Cannot find association request: "+id)
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

	org.Delete(assocReqColl, id)
	if body.Response == "accept" {
		org.Put(assocColl, user, mustEncode(map[string]any{"username": user}))
		addUserToOrgGroup(org, "users", user)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"organization": map[string]any{"name": org.Name()},
	})
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
