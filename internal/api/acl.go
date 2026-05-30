package api

import (
	"encoding/json"
	"net/http"
	"slices"

	"github.com/tas50/cinc-zero/internal/store"
)

// ACLs in cinc-zero are structural, not enforced (matching the permissive authz
// model): every object exposes a well-formed five-permission ACL that tooling
// such as `knife acl` can read and write, but access is never actually denied.
// ACLs are stored per object in the "acls" collection keyed by "type/name"; an
// object with no stored ACL reports a sensible permissive default.

var aclPerms = []string{"create", "read", "update", "delete", "grant"}

// aclObjectTypes are the object types Chef exposes ACL endpoints for.
var aclObjectTypes = []string{
	"clients", "containers", "cookbooks", "cookbook_artifacts", "data",
	"environments", "groups", "nodes", "policies", "policy_groups", "roles",
}

func (a *API) registerACLRoutes(mux *http.ServeMux) {
	for _, typ := range aclObjectTypes {
		base := "/organizations/{org}/" + typ + "/{name}/_acl"
		mux.HandleFunc("GET "+base, a.getACL(typ))
		mux.HandleFunc("GET "+base+"/{perm}", a.getACLPerm(typ))
		mux.HandleFunc("PUT "+base+"/{perm}", a.putACLPerm(typ))
	}
	// The organization's own ACL.
	mux.HandleFunc("GET /organizations/{org}/_acl", a.getOrgACL)
	mux.HandleFunc("GET /organizations/{org}/_acl/{perm}", a.getOrgACLPerm)
	mux.HandleFunc("PUT /organizations/{org}/_acl/{perm}", a.putOrgACLPerm)
}

func aclKey(typ, name string) string { return typ + "/" + name }

// defaultACL returns the permissive default ACL granted to a fresh object.
func defaultACL() map[string]any {
	perm := func(groups ...string) map[string]any {
		return map[string]any{"actors": []string{}, "groups": groups}
	}
	return map[string]any{
		"create": perm("admins", "users"),
		"read":   perm("admins", "users", "clients"),
		"update": perm("admins", "users"),
		"delete": perm("admins", "users"),
		"grant":  perm("admins"),
	}
}

func loadACL(org *store.Org, typ, name string) map[string]any {
	if raw, ok := org.Get("acls", aclKey(typ, name)); ok {
		var m map[string]any
		if json.Unmarshal(raw, &m) == nil {
			return m
		}
	}
	return defaultACL()
}

func (a *API) getACL(typ string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.writeACL(w, r, typ, r.PathValue("name"))
	}
}

func (a *API) getACLPerm(typ string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.writeACLPerm(w, r, typ, r.PathValue("name"))
	}
}

func (a *API) putACLPerm(typ string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.updateACLPerm(w, r, typ, r.PathValue("name"))
	}
}

func (a *API) getOrgACL(w http.ResponseWriter, r *http.Request) {
	a.writeACL(w, r, "organizations", r.PathValue("org"))
}

func (a *API) getOrgACLPerm(w http.ResponseWriter, r *http.Request) {
	a.writeACLPerm(w, r, "organizations", r.PathValue("org"))
}

func (a *API) putOrgACLPerm(w http.ResponseWriter, r *http.Request) {
	a.updateACLPerm(w, r, "organizations", r.PathValue("org"))
}

func (a *API) writeACL(w http.ResponseWriter, r *http.Request, typ, name string) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	writeJSON(w, http.StatusOK, loadACL(org, typ, name))
}

func (a *API) writeACLPerm(w http.ResponseWriter, r *http.Request, typ, name string) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	perm := r.PathValue("perm")
	if !slices.Contains(aclPerms, perm) {
		writeError(w, http.StatusNotFound, "Cannot find ACL permission "+perm)
		return
	}
	acl := loadACL(org, typ, name)
	writeJSON(w, http.StatusOK, map[string]any{perm: acl[perm]})
}

func (a *API) updateACLPerm(w http.ResponseWriter, r *http.Request, typ, name string) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	perm := r.PathValue("perm")
	if !slices.Contains(aclPerms, perm) {
		writeError(w, http.StatusNotFound, "Cannot find ACL permission "+perm)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	// The body is either {"<perm>": {actors, groups}} or the ace object itself.
	ace := body
	if inner, ok := body[perm].(map[string]any); ok {
		ace = inner
	}

	acl := loadACL(org, typ, name)
	acl[perm] = ace
	org.Put("acls", aclKey(typ, name), mustEncode(acl))
	writeJSON(w, http.StatusOK, map[string]any{perm: ace})
}
