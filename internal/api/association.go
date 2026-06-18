package api

import (
	"encoding/json"
	"net/http"

	"github.com/tas50/cinc-zero/internal/store"
)

// User↔organization association. A global user is associated with an org by
// recording their username in the org's "association_users" collection.

// AssociationUsersCollection holds the usernames associated with an org (org
// membership). Only the key matters; the value is a small record.
const AssociationUsersCollection = "association_users"

const assocColl = AssociationUsersCollection

func (a *API) registerAssociationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /organizations/{org}/users", a.listOrgUsers)
	mux.HandleFunc("POST /organizations/{org}/users", a.associateUser)
	mux.HandleFunc("GET /organizations/{org}/users/{user}", a.getOrgUser)
	mux.HandleFunc("DELETE /organizations/{org}/users/{user}", a.disassociateUser)
	mux.HandleFunc("GET /users/{user}/organizations", a.listUserOrgs)
}

// orgViewAllowed reports whether the request's actor may view this org's
// membership. With no actor (auth disabled / API-layer) everything is allowed;
// the bootstrap superuser always may; otherwise the actor must currently be a
// member of the org. A non-member — for example a user who was removed — is
// refused with 403, matching Chef Infra Server.
func (a *API) orgViewAllowed(w http.ResponseWriter, r *http.Request, org *store.Org) bool {
	actor, ok := actorFromContext(r.Context())
	if !ok || actor.IsGlobalAdmin {
		return true
	}
	if _, ok := org.Get(assocColl, actor.Name); ok {
		return true
	}
	writeError(w, http.StatusForbidden, "You are not a member of this organization.")
	return false
}

func (a *API) listOrgUsers(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	if !a.orgViewAllowed(w, r, org) {
		return
	}
	out := make([]map[string]any, 0)
	for _, username := range org.Keys(assocColl) {
		out = append(out, map[string]any{"user": map[string]any{"username": username}})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) associateUser(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	username, _ := body["username"].(string)
	if username == "" {
		username, _ = body["user"].(string)
	}
	if username == "" {
		writeError(w, http.StatusBadRequest, "Field 'username' missing")
		return
	}
	// The user must exist globally before it can be associated.
	if _, ok := a.store.Global().Get("users", username); !ok {
		writeError(w, http.StatusNotFound, "Cannot find user "+username)
		return
	}
	// Associating a user who is already a member is a conflict.
	if _, ok := org.Get(assocColl, username); ok {
		writeStringError(w, http.StatusConflict, "The association already exists.")
		return
	}
	org.Put(assocColl, username, mustEncode(map[string]any{"username": username}))
	writeJSON(w, http.StatusCreated, map[string]any{
		"uri": requestBaseURL(r) + "/organizations/" + org.Name() + "/users/" + username,
	})
}

func (a *API) getOrgUser(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	if !a.orgViewAllowed(w, r, org) {
		return
	}
	user := r.PathValue("user")
	if _, ok := org.Get(assocColl, user); !ok {
		writeError(w, http.StatusNotFound, "Cannot find user "+user+" in organization "+org.Name())
		return
	}
	// Return the global user record (without its out-of-band password).
	if raw, ok := a.store.Global().Get("users", user); ok {
		writeRaw(w, http.StatusOK, raw)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"username": user})
}

func (a *API) disassociateUser(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	user := r.PathValue("user")
	if _, ok := org.Delete(assocColl, user); !ok {
		writeError(w, http.StatusNotFound, "Cannot find user "+user+" in organization "+org.Name())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"username": user})
}

func (a *API) listUserOrgs(w http.ResponseWriter, r *http.Request) {
	user := r.PathValue("user")
	out := make([]map[string]any, 0)
	for _, name := range a.store.ListOrgs() {
		org, ok := a.store.Org(name)
		if !ok {
			continue
		}
		if _, ok := org.Get(assocColl, user); ok {
			out = append(out, map[string]any{
				"organization": map[string]any{"name": name, "full_name": name},
			})
		}
	}
	writeJSON(w, http.StatusOK, out)
}
