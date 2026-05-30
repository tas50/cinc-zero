package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/tas50/cinc-zero/internal/store"
)

// cookbook_artifacts hold the immutable, content-addressed cookbooks that
// Policyfiles pin to. They mirror cookbooks but are keyed by an opaque
// identifier rather than a version, share the same blob store and manifest
// shape, and have no _latest/_recipes aliases.

func (a *API) registerCookbookArtifactRoutes(mux *http.ServeMux) {
	const base = "/organizations/{org}/cookbook_artifacts"
	mux.HandleFunc("GET "+base, a.listArtifacts)
	mux.HandleFunc("GET "+base+"/{name}", a.getArtifact)
	mux.HandleFunc("GET "+base+"/{name}/{identifier}", a.getArtifactVersion)
	mux.HandleFunc("PUT "+base+"/{name}/{identifier}", a.putArtifactVersion)
	mux.HandleFunc("DELETE "+base+"/{name}/{identifier}", a.deleteArtifactVersion)
}

// artifactIdentifiers returns name -> identifiers (sorted), since artifact
// identifiers are opaque hashes with no meaningful version ordering.
func artifactIdentifiers(org *store.Org) map[string][]string {
	out := map[string][]string{}
	for _, key := range org.Keys("cookbook_artifacts") {
		name, ident, ok := strings.Cut(key, "/")
		if !ok {
			continue
		}
		out[name] = append(out[name], ident)
	}
	for _, idents := range out {
		sort.Strings(idents)
	}
	return out
}

func (a *API) listArtifacts(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	writeJSON(w, http.StatusOK, collectionListBody(r, org, "cookbook_artifacts", "identifier", artifactIdentifiers(org)))
}

func (a *API) getArtifact(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	name := r.PathValue("name")
	idents, ok := artifactIdentifiers(org)[name]
	if !ok {
		writeError(w, http.StatusNotFound, "Cannot find a cookbook artifact named "+name)
		return
	}
	writeJSON(w, http.StatusOK, collectionListBody(r, org, "cookbook_artifacts", "identifier", map[string][]string{name: idents}))
}

func (a *API) getArtifactVersion(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	name, ident := r.PathValue("name"), r.PathValue("identifier")
	raw, ok := org.Get("cookbook_artifacts", cookbookKey(name, ident))
	if !ok {
		writeError(w, http.StatusNotFound, "Cannot find a cookbook artifact named "+name+" with identifier "+ident)
		return
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		writeRaw(w, http.StatusOK, raw)
		return
	}
	injectFileURLs(m, r, org.Name())
	writeJSON(w, http.StatusOK, m)
}

func (a *API) putArtifactVersion(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	name, ident := r.PathValue("name"), r.PathValue("identifier")
	var m map[string]any
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	for _, sum := range manifestChecksums(m) {
		if !org.HasBlob(sum) {
			writeError(w, http.StatusBadRequest, "Manifest has a checksum that hasn't been uploaded: "+sum)
			return
		}
	}
	_, existed := org.Get("cookbook_artifacts", cookbookKey(name, ident))
	org.Put("cookbook_artifacts", cookbookKey(name, ident), mustEncode(m))

	status := http.StatusCreated
	if existed {
		status = http.StatusOK
	}
	injectFileURLs(m, r, org.Name())
	writeJSON(w, status, m)
}

func (a *API) deleteArtifactVersion(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	name, ident := r.PathValue("name"), r.PathValue("identifier")
	raw, ok := org.Delete("cookbook_artifacts", cookbookKey(name, ident))
	if !ok {
		writeError(w, http.StatusNotFound, "Cannot find a cookbook artifact named "+name+" with identifier "+ident)
		return
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) == nil {
		injectFileURLs(m, r, org.Name())
		writeJSON(w, http.StatusOK, m)
		return
	}
	writeRaw(w, http.StatusOK, raw)
}
