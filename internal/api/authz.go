package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/tas50/cinc-zero/internal/store"
)

// Authorization in cinc-zero is structural, not enforced: groups and containers
// are stored and returned faithfully (so tooling that reads or writes them
// behaves), but every authenticated actor is treated as authorized. This
// matches the design's permissive default for a test server.

var defaultGroups = []string{"admins", "billing-admins", "clients", "users"}

var defaultContainers = []string{
	"clients", "containers", "cookbooks", "cookbook_artifacts", "data",
	"environments", "groups", "nodes", "policies", "policy_groups",
	"roles", "sandboxes",
}

// seedAuthz adds the default groups and containers Chef creates in every org.
func seedAuthz(org *store.Org) {
	for _, g := range defaultGroups {
		if _, ok := org.Get("groups", g); !ok {
			org.Put("groups", g, fmt.Appendf(nil,
				`{"name":%q,"groupname":%q,"actors":[],"users":[],"clients":[],"groups":[]}`, g, g))
		}
	}
	for _, c := range defaultContainers {
		if _, ok := org.Get("containers", c); !ok {
			org.Put("containers", c, fmt.Appendf(nil,
				`{"containername":%q,"containerpath":%q}`, c, c))
		}
	}
}

func (a *API) registerAuthzRoutes(mux *http.ServeMux) {
	// Groups are name-keyed JSON objects: the generic object CRUD fits.
	a.registerObjectRoutes(mux, "groups")

	// Containers are keyed by "containername"; list and read are what tooling
	// needs, plus create/delete for completeness.
	const base = "/organizations/{org}/containers"
	mux.HandleFunc("GET "+base, a.listObjects("containers"))
	mux.HandleFunc("GET "+base+"/{name}", a.getObject("containers"))
	mux.HandleFunc("POST "+base, a.createContainer)
	mux.HandleFunc("DELETE "+base+"/{name}", a.deleteObject("containers"))
}

func (a *API) createContainer(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	var obj map[string]any
	if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	name, _ := obj["containername"].(string)
	if name == "" {
		name, _ = obj["id"].(string)
	}
	if name == "" {
		writeError(w, http.StatusBadRequest, "Field 'containername' missing")
		return
	}
	org.Put("containers", name, mustEncode(obj))
	writeJSON(w, http.StatusCreated, map[string]string{
		"uri": objectURL(r, org.Name(), "containers", name),
	})
}
