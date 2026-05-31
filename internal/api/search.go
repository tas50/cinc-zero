package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"sync"

	"github.com/tas50/cinc-zero/internal/search"
	"github.com/tas50/cinc-zero/internal/store"
)

// searchCache memoizes the flattened searchable view of stored documents.
//
// Flattening (and the node attribute-precedence merge) is the dominant per-query
// cost, and a collection is usually searched far more often than it is written.
// Each entry is validated against the store's backing slice by identity: the
// store never mutates a value in place (Put writes a fresh slice), so an entry
// whose raw slice still aliases the stored one is known current. A write
// replaces the slice, so the stale entry simply fails the identity check and is
// recomputed — no explicit invalidation is needed. Entries for deleted objects
// linger but are bounded by the set of distinct keys ever searched; cinc-zero
// is a short-lived in-memory test server, so this is acceptable.
type searchCache struct {
	mu sync.Mutex
	m  map[string]searchEntry
}

type searchEntry struct {
	raw    []byte // identity-checked against the current stored slice
	merged map[string]any
	fields map[string][]string
}

func newSearchCache() *searchCache {
	return &searchCache{m: make(map[string]searchEntry)}
}

// searchDoc returns the searchable view of a stored document: its merged form
// (a node's attribute-precedence merge when mergeAttrs is set, otherwise the
// decoded object) and its flattened field map. Results are cached and reused
// while the underlying stored bytes are unchanged. ok is false when raw is not
// decodable JSON, in which case the caller skips the document.
func (a *API) searchDoc(coll, id string, raw []byte, mergeAttrs bool) (merged map[string]any, fields map[string][]string, ok bool) {
	key := coll + "\x00" + id

	a.search.mu.Lock()
	if e, hit := a.search.m[key]; hit && sameBytes(e.raw, raw) {
		a.search.mu.Unlock()
		return e.merged, e.fields, true
	}
	a.search.mu.Unlock()

	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		return nil, nil, false
	}
	searchable := doc
	if mergeAttrs {
		searchable = nodeSearchDoc(doc)
	}
	fields = search.Flatten(searchable)

	a.search.mu.Lock()
	a.search.m[key] = searchEntry{raw: raw, merged: searchable, fields: fields}
	a.search.mu.Unlock()
	return searchable, fields, true
}

// sameBytes reports whether two slices share the same backing array (and length),
// i.e. are the identical stored value rather than merely equal in content.
func sameBytes(a, b []byte) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

// match is one search hit: its id, the raw stored object (returned verbatim for
// whole-object results), and its merged form (for partial-search projection).
type match struct {
	id     string
	raw    json.RawMessage
	merged map[string]any
}

// collectMatches scans an index's collection and returns the documents matching
// query, sorted by id. It reads each document copy-free under a single lock and
// reuses cached flattened views for unchanged objects.
func (a *API) collectMatches(org *store.Org, idx searchIndex, query search.Query) []match {
	var matches []match
	org.Range(idx.collection, func(id string, raw []byte) bool {
		merged, fields, ok := a.searchDoc(idx.collection, id, raw, idx.mergeAttrs)
		if ok && query.Matches(fields) {
			matches = append(matches, match{id: id, raw: raw, merged: merged})
		}
		return true
	})
	sort.Slice(matches, func(i, j int) bool { return matches[i].id < matches[j].id })
	return matches
}

// Search is served in-process: matching objects are flattened the way Chef's
// indexer would and filtered by a parsed Solr query. The built-in indexes are
// node, role, client, and environment; any other index name is treated as a
// data bag. The node index searches deep-merged attributes (default < normal <
// override < automatic) plus top-level fields, matching chef-client.

const defaultSearchRows = 1000

func (a *API) registerSearchRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /organizations/{org}/search", a.listSearchIndexes)
	mux.HandleFunc("GET /organizations/{org}/search/{index}", a.runSearch)
	mux.HandleFunc("POST /organizations/{org}/search/{index}", a.runSearch)
}

func (a *API) listSearchIndexes(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	base := requestBaseURL(r) + "/organizations/" + org.Name() + "/search/"
	out := map[string]string{
		"node":        base + "node",
		"role":        base + "role",
		"client":      base + "client",
		"environment": base + "environment",
	}
	for _, bag := range org.Keys(dataBagsColl) {
		out[bag] = base + bag
	}
	writeJSON(w, http.StatusOK, out)
}

// searchIndex describes where an index's documents live and how to address one.
type searchIndex struct {
	collection string
	mergeAttrs bool // node-style attribute precedence merge
	urlFor     func(r *http.Request, org, id string) string
}

func (a *API) resolveIndex(r *http.Request, org *store.Org, name string) (searchIndex, bool) {
	switch name {
	case "node", "role", "client", "environment":
		coll := name + "s"
		return searchIndex{
			collection: coll,
			mergeAttrs: name == "node",
			urlFor:     func(r *http.Request, org, id string) string { return objectURL(r, org, coll, id) },
		}, true
	default:
		if _, ok := org.View(dataBagsColl, name); !ok {
			return searchIndex{}, false
		}
		return searchIndex{
			collection: dataBagItemsColl(name),
			urlFor:     func(r *http.Request, org, id string) string { return dataBagURL(r, org, name) + "/" + id },
		}, true
	}
}

func (a *API) runSearch(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	indexName := r.PathValue("index")
	idx, ok := a.resolveIndex(r, org, indexName)
	if !ok {
		writeError(w, http.StatusNotFound, "I don't know how to search for "+indexName+" data objects.")
		return
	}

	qStr := r.URL.Query().Get("q")
	if qStr == "" {
		qStr = "*:*"
	}
	query, err := search.Parse(qStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid search query: "+err.Error())
		return
	}

	// Partial search (POST body of result-key -> attribute path) is applied per
	// matching row; an empty or absent body yields whole-object results.
	var partial map[string][]string
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&partial); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid partial search body")
			return
		}
	}

	matches := a.collectMatches(org, idx, query)

	start := queryInt(r, "start", 0)
	rows := queryInt(r, "rows", defaultSearchRows)
	total := len(matches)
	window := matches[clamp(start, 0, total):clamp(start+rows, 0, total)]

	out := make([]any, 0, len(window))
	for _, m := range window {
		if partial != nil {
			data := map[string]any{}
			for key, path := range partial {
				if len(path) == 0 {
					data[key] = nil // empty projection: no value for this key
					continue
				}
				data[key] = traversePath(m.merged, path)
			}
			out = append(out, map[string]any{
				"url":  idx.urlFor(r, org.Name(), m.id),
				"data": data,
			})
		} else {
			out = append(out, m.raw)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"total": total,
		"start": start,
		"rows":  out,
	})
}

// nodeSearchDoc produces the searchable view of a node: its attribute
// precedence levels deep-merged (default < normal < override < automatic),
// overlaid on the node's top-level fields (name, chef_environment, run_list…).
func nodeSearchDoc(node map[string]any) map[string]any {
	merged := map[string]any{}
	for _, level := range []string{"default", "normal", "override", "automatic"} {
		if attrs, ok := node[level].(map[string]any); ok {
			deepMerge(merged, attrs)
		}
	}
	out := map[string]any{}
	for k, v := range node {
		out[k] = v
	}
	for k, v := range merged {
		out[k] = v
	}
	return out
}

// deepMerge recursively merges src into dst, with src winning on scalar keys.
func deepMerge(dst, src map[string]any) {
	for k, v := range src {
		if sub, ok := v.(map[string]any); ok {
			if existing, ok := dst[k].(map[string]any); ok {
				deepMerge(existing, sub)
				continue
			}
			cp := map[string]any{}
			deepMerge(cp, sub)
			dst[k] = cp
			continue
		}
		dst[k] = v
	}
}

// traversePath walks doc following path, returning nil if any segment is absent.
func traversePath(doc map[string]any, path []string) any {
	var cur any = doc
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = m[key]
		if !ok {
			return nil
		}
	}
	return cur
}

func queryInt(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return def
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
