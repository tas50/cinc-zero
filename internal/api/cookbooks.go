package api

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tas50/cinc-zero/internal/store"
)

// Cookbook upload in Chef is a three-step flow: a client announces the file
// checksums it has via POST /sandboxes, uploads the missing ones to the file
// store, commits the sandbox, then PUTs a cookbook manifest that references
// those checksums. Files live in an in-memory blob store keyed by hex MD5;
// manifests live in the "cookbooks" collection keyed by "name/version".

func (a *API) registerCookbookRoutes(mux *http.ServeMux) {
	a.registerFileStoreRoutes(mux)
	a.registerSandboxRoutes(mux)

	const base = "/organizations/{org}/cookbooks"
	mux.HandleFunc("GET "+base, a.listCookbooks)
	mux.HandleFunc("GET "+base+"/_latest", a.cookbooksLatest)
	mux.HandleFunc("GET "+base+"/_recipes", a.cookbookRecipesEndpoint)
	mux.HandleFunc("GET "+base+"/{name}", a.getCookbook)
	mux.HandleFunc("GET "+base+"/{name}/{version}", a.getCookbookVersion)
	mux.HandleFunc("PUT "+base+"/{name}/{version}", a.putCookbookVersion)
	mux.HandleFunc("DELETE "+base+"/{name}/{version}", a.deleteCookbookVersion)

	mux.HandleFunc("GET /organizations/{org}/universe", a.universe)
}

// universe reports every cookbook version with its dependency constraints and
// download location, as consumed by Policyfile/Berkshelf dependency solvers.
func (a *API) universe(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	out := map[string]any{}
	for name, vers := range cookbookVersions(org) {
		versions := map[string]any{}
		for _, v := range vers {
			raw, ok := org.Get("cookbooks", cookbookKey(name, v))
			if !ok {
				continue
			}
			var m map[string]any
			if json.Unmarshal(raw, &m) != nil {
				continue
			}
			url := cookbookBaseURL(r, org.Name(), name) + "/" + v
			versions[v] = map[string]any{
				"location_type": "chef_server",
				"location_path": url,
				"download_url":  url,
				"dependencies":  manifestDependencies(m),
			}
		}
		out[name] = versions
	}
	writeJSON(w, http.StatusOK, out)
}

// manifestDependencies extracts metadata.dependencies, defaulting to an empty
// object so the universe entry always carries a dependencies map.
func manifestDependencies(m map[string]any) map[string]any {
	if md, ok := m["metadata"].(map[string]any); ok {
		if deps, ok := md["dependencies"].(map[string]any); ok {
			return deps
		}
	}
	return map[string]any{}
}

// --- file store -----------------------------------------------------------

func (a *API) registerFileStoreRoutes(mux *http.ServeMux) {
	const base = "/organizations/{org}/file_store/{checksum}"
	mux.HandleFunc("PUT "+base, a.putFile)
	mux.HandleFunc("GET "+base, a.getFile)
}

func fileStoreURL(r *http.Request, org, checksum string) string {
	return requestBaseURL(r) + "/organizations/" + org + "/file_store/" + checksum
}

func (a *API) putFile(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	checksum := r.PathValue("checksum")
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	sum := md5.Sum(body)
	if hex.EncodeToString(sum[:]) != checksum {
		writeError(w, http.StatusBadRequest, "Checksum of uploaded content does not match "+checksum)
		return
	}
	org.PutBlob(checksum, body)
	w.WriteHeader(http.StatusOK)
}

func (a *API) getFile(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	data, ok := org.Blob(r.PathValue("checksum"))
	if !ok {
		writeError(w, http.StatusNotFound, "File not found")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// --- sandboxes ------------------------------------------------------------

func (a *API) registerSandboxRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /organizations/{org}/sandboxes", a.createSandbox)
	mux.HandleFunc("PUT /organizations/{org}/sandboxes/{id}", a.commitSandbox)
}

func (a *API) createSandbox(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	var req struct {
		Checksums map[string]any `json:"checksums"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	sums := make([]string, 0, len(req.Checksums))
	for sum := range req.Checksums {
		sums = append(sums, sum)
	}
	sort.Strings(sums)

	id := sandboxID(sums)
	out := map[string]any{}
	for _, sum := range sums {
		if org.HasBlob(sum) {
			out[sum] = map[string]any{"needs_upload": false}
		} else {
			out[sum] = map[string]any{"url": fileStoreURL(r, org.Name(), sum), "needs_upload": true}
		}
	}

	doc := map[string]any{
		"sandbox_id":   id,
		"checksums":    sums,
		"is_completed": false,
		"create_time":  time.Now().UTC().Format(time.RFC3339),
	}
	org.Put("sandboxes", id, mustEncode(doc))

	writeJSON(w, http.StatusCreated, map[string]any{
		"sandbox_id": id,
		"uri":        requestBaseURL(r) + "/organizations/" + org.Name() + "/sandboxes/" + id,
		"checksums":  out,
	})
}

func (a *API) commitSandbox(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	id := r.PathValue("id")
	raw, ok := org.Get("sandboxes", id)
	if !ok {
		writeError(w, http.StatusNotFound, "Cannot find sandbox "+id)
		return
	}
	var doc struct {
		Checksums []string `json:"checksums"`
	}
	json.Unmarshal(raw, &doc)

	for _, sum := range doc.Checksums {
		if !org.HasBlob(sum) {
			writeError(w, http.StatusBadRequest, "Checksum "+sum+" was not uploaded before the sandbox was committed")
			return
		}
	}

	committed := map[string]any{
		"guid":         id,
		"name":         id,
		"checksums":    doc.Checksums,
		"is_completed": true,
	}
	org.Put("sandboxes", id, mustEncode(committed))
	writeJSON(w, http.StatusOK, committed)
}

// sandboxID derives a deterministic id from the (sorted) checksum set, matching
// chef-zero's content-addressed sandbox identifiers.
func sandboxID(sortedSums []string) string {
	sum := md5.Sum([]byte(strings.Join(sortedSums, "")))
	return hex.EncodeToString(sum[:])
}

// --- cookbooks ------------------------------------------------------------

func cookbookKey(name, version string) string { return name + "/" + version }

// collectionItemURL builds the URL for a versioned-collection item such as
// /organizations/{org}/cookbooks/{name} (shared by cookbooks and artifacts).
func collectionItemURL(r *http.Request, org, segment, name string) string {
	return requestBaseURL(r) + "/organizations/" + org + "/" + segment + "/" + name
}

func cookbookBaseURL(r *http.Request, org, name string) string {
	return collectionItemURL(r, org, "cookbooks", name)
}

// cookbookVersions returns name -> versions (each list sorted newest first).
func cookbookVersions(org *store.Org) map[string][]string {
	out := map[string][]string{}
	for _, key := range org.Keys("cookbooks") {
		name, version, ok := strings.Cut(key, "/")
		if !ok {
			continue
		}
		out[name] = append(out[name], version)
	}
	for _, vers := range out {
		sort.Slice(vers, func(i, j int) bool { return compareVersions(vers[i], vers[j]) > 0 })
	}
	return out
}

func (a *API) listCookbooks(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	writeJSON(w, http.StatusOK, collectionListBody(r, org, "cookbooks", "version", cookbookVersions(org)))
}

func (a *API) getCookbook(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	name := r.PathValue("name")
	all := cookbookVersions(org)
	vers, ok := all[name]
	if !ok {
		writeError(w, http.StatusNotFound, "Cannot find a cookbook named "+name)
		return
	}
	writeJSON(w, http.StatusOK, collectionListBody(r, org, "cookbooks", "version", map[string][]string{name: vers}))
}

// collectionListBody builds the Chef "name -> {url, versions:[...]}" structure
// shared by cookbooks and cookbook_artifacts. label is the per-entry key that
// names the item ("version" for cookbooks, "identifier" for artifacts). The
// num_versions query parameter ("all" or an integer) caps each list.
func collectionListBody(r *http.Request, org *store.Org, segment, label string, items map[string][]string) map[string]any {
	limit := -1
	if nv := r.URL.Query().Get("num_versions"); nv != "" && nv != "all" {
		if n, err := strconv.Atoi(nv); err == nil {
			limit = n
		}
	}
	out := map[string]any{}
	for name, vals := range items {
		if limit >= 0 && len(vals) > limit {
			vals = vals[:limit]
		}
		entries := make([]map[string]string, 0, len(vals))
		for _, v := range vals {
			entries = append(entries, map[string]string{
				"url": collectionItemURL(r, org.Name(), segment, name) + "/" + v,
				label: v,
			})
		}
		out[name] = map[string]any{
			"url":      collectionItemURL(r, org.Name(), segment, name),
			"versions": entries,
		}
	}
	return out
}

// resolveVersion maps the "_latest" alias to the newest stored version.
func resolveVersion(org *store.Org, name, version string) (string, bool) {
	if version != "_latest" && version != "latest" {
		return version, true
	}
	vers := cookbookVersions(org)[name]
	if len(vers) == 0 {
		return "", false
	}
	return vers[0], true // sorted newest first
}

func (a *API) getCookbookVersion(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	name := r.PathValue("name")
	version, ok := resolveVersion(org, name, r.PathValue("version"))
	if !ok {
		writeError(w, http.StatusNotFound, "Cannot find a cookbook named "+name)
		return
	}
	raw, ok := org.Get("cookbooks", cookbookKey(name, version))
	if !ok {
		writeError(w, http.StatusNotFound, "Cannot find a cookbook named "+name+" version "+version)
		return
	}

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		writeRaw(w, http.StatusOK, raw)
		return
	}
	injectFileURLs(m, r, org.Name())
	writeJSON(w, http.StatusOK, m)
}

func (a *API) putCookbookVersion(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	name := r.PathValue("name")
	version := r.PathValue("version")

	var m map[string]any
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Every referenced file must already be in the blob store.
	for _, sum := range manifestChecksums(m) {
		if !org.HasBlob(sum) {
			writeError(w, http.StatusBadRequest, "Manifest has a checksum that hasn't been uploaded: "+sum)
			return
		}
	}

	_, existed := org.Get("cookbooks", cookbookKey(name, version))
	org.Put("cookbooks", cookbookKey(name, version), mustEncode(m))

	status := http.StatusCreated
	if existed {
		status = http.StatusOK
	}
	injectFileURLs(m, r, org.Name())
	writeJSON(w, status, m)
}

func (a *API) deleteCookbookVersion(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	name := r.PathValue("name")
	version, ok := resolveVersion(org, name, r.PathValue("version"))
	if !ok {
		writeError(w, http.StatusNotFound, "Cannot find a cookbook named "+name)
		return
	}
	raw, ok := org.Delete("cookbooks", cookbookKey(name, version))
	if !ok {
		writeError(w, http.StatusNotFound, "Cannot find a cookbook named "+name+" version "+version)
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

func (a *API) cookbooksLatest(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	out := map[string]string{}
	for name, vers := range cookbookVersions(org) {
		if len(vers) > 0 {
			out[name] = cookbookBaseURL(r, org.Name(), name) + "/" + vers[0]
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) cookbookRecipesEndpoint(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	seen := map[string]bool{}
	var recipes []string
	for name, vers := range cookbookVersions(org) {
		if len(vers) == 0 {
			continue
		}
		raw, ok := org.Get("cookbooks", cookbookKey(name, vers[0]))
		if !ok {
			continue
		}
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		for _, rec := range manifestRecipes(m, name) {
			if !seen[rec] {
				seen[rec] = true
				recipes = append(recipes, rec)
			}
		}
	}
	sort.Strings(recipes)
	writeJSON(w, http.StatusOK, recipes)
}

// --- manifest helpers -----------------------------------------------------

// walkFileEntries invokes fn for every object in the manifest that carries a
// "checksum" string. This is shape-agnostic: it handles both the modern
// "all_files" manifest and the older per-segment ("recipes", "files", ...)
// layout.
func walkFileEntries(v any, fn func(obj map[string]any)) {
	switch t := v.(type) {
	case map[string]any:
		if _, ok := t["checksum"].(string); ok {
			fn(t)
		}
		for _, val := range t {
			walkFileEntries(val, fn)
		}
	case []any:
		for _, val := range t {
			walkFileEntries(val, fn)
		}
	}
}

func manifestChecksums(m map[string]any) []string {
	var sums []string
	walkFileEntries(m, func(obj map[string]any) {
		if sum, ok := obj["checksum"].(string); ok {
			sums = append(sums, sum)
		}
	})
	return sums
}

func injectFileURLs(m map[string]any, r *http.Request, org string) {
	walkFileEntries(m, func(obj map[string]any) {
		if sum, ok := obj["checksum"].(string); ok {
			obj["url"] = fileStoreURL(r, org, sum)
		}
	})
}

// manifestRecipes returns the run-list recipe names for a cookbook manifest:
// "<cookbook>" for the default recipe and "<cookbook>::<name>" otherwise.
func manifestRecipes(m map[string]any, cookbook string) []string {
	var recipes []string
	walkFileEntries(m, func(obj map[string]any) {
		path, _ := obj["path"].(string)
		if !strings.HasPrefix(path, "recipes/") || !strings.HasSuffix(path, ".rb") {
			return
		}
		base := strings.TrimSuffix(strings.TrimPrefix(path, "recipes/"), ".rb")
		if base == "default" {
			recipes = append(recipes, cookbook)
		} else {
			recipes = append(recipes, cookbook+"::"+base)
		}
	})
	return recipes
}

// compareVersions orders dotted numeric versions (e.g. "1.2.0" vs "1.10.0"),
// returning -1, 0, or 1. Missing trailing segments are treated as zero.
func compareVersions(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		av, bv := 0, 0
		if i < len(as) {
			av, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bv, _ = strconv.Atoi(bs[i])
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}
