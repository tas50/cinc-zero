package api

import (
	"encoding/json"
	"net/http"

	"github.com/tas50/cinc-zero/internal/store"
)

// authenticate_user validates a user's password. Passwords are stored
// out-of-band (never in the user object or its API responses) in the
// "passwords" collection of the actor's scope, keyed by name. This is a test
// server, so the password is kept as-is in memory; it is only ever compared,
// never returned.

const passwordsColl = "passwords"

// stashPassword moves a "password" field out of an actor object into the
// out-of-band password store, so it is neither persisted in nor returned with
// the actor record.
func stashPassword(org *store.Org, name string, obj map[string]any) {
	if pw, ok := obj["password"].(string); ok {
		org.Put(passwordsColl, name, []byte(pw))
		delete(obj, "password")
	}
}

func (a *API) registerAuthenticateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /authenticate_user", a.authenticateUser)
}

func (a *API) authenticateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	global := a.store.Global()
	userRaw, ok := global.Get("users", body.Name)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Failed to authenticate. Username and password incorrect.")
		return
	}
	stored, ok := global.Get(passwordsColl, body.Name)
	if !ok || string(stored) != body.Password {
		writeError(w, http.StatusUnauthorized, "Failed to authenticate. Username and password incorrect.")
		return
	}

	var user map[string]any
	json.Unmarshal(userRaw, &user)
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "linked",
		"user":   user,
	})
}
