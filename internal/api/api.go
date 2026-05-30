// Package api implements the Chef Infra Server HTTP API surface over the
// in-memory store. Handlers operate on a *store.Org and emit Chef-shaped JSON.
// Authentication is a separate layer wrapped around this handler, so these
// handlers can be exercised directly in tests.
package api

import (
	"net/http"

	"github.com/tas50/cinc-zero/internal/store"
)

// API holds the dependencies shared by all handlers.
type API struct {
	store *store.Store
}

// New returns an API backed by st.
func New(st *store.Store) *API {
	return &API{store: st}
}

// Handler builds the HTTP handler exposing the full API surface.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	a.registerSystemRoutes(mux)
	a.registerObjectRoutes(mux, "nodes")
	a.registerObjectRoutes(mux, "roles")
	a.registerEnvironmentRoutes(mux)

	orgScope := func(w http.ResponseWriter, r *http.Request) *store.Org { return a.org(w, r) }
	globalScope := func(http.ResponseWriter, *http.Request) *store.Org { return a.store.Global() }
	a.registerActorRoutes(mux, "/organizations/{org}/", "clients", orgScope)
	a.registerActorRoutes(mux, "/", "users", globalScope)
	a.registerDataBagRoutes(mux)
	a.registerPolicyRoutes(mux)
	return mux
}

// org resolves the {org} path value to its store, writing a 404 and returning
// nil if it does not exist.
func (a *API) org(w http.ResponseWriter, r *http.Request) *store.Org {
	name := r.PathValue("org")
	org, ok := a.store.Org(name)
	if !ok {
		writeError(w, http.StatusNotFound, "Cannot find org "+name)
		return nil
	}
	return org
}
