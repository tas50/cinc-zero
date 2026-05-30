package api

import (
	"encoding/json"
	"net/http"
)

// User↔organization association. A global user is associated with an org by
// recording their username in the org's "association_users" collection. (The
// invite-based association_requests flow is a separate, future increment.)

const assocColl = "association_users"

func (a *API) registerAssociationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /organizations/{org}/users", a.listOrgUsers)
	mux.HandleFunc("POST /organizations/{org}/users", a.associateUser)
	mux.HandleFunc("GET /organizations/{org}/users/{user}", a.getOrgUser)
	mux.HandleFunc("DELETE /organizations/{org}/users/{user}", a.disassociateUser)
	mux.HandleFunc("GET /users/{user}/organizations", a.listUserOrgs)
}

func (a *API) listOrgUsers(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
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
