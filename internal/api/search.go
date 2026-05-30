package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"

	"github.com/tas50/cinc-zero/internal/search"
	"github.com/tas50/cinc-zero/internal/store"
)

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
		if _, ok := org.Get(dataBagsColl, name); !ok {
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
	// matching row; an empty/absent body yields whole-object results.
	var partial map[string][]string
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&partial); err != nil {
			writeError(w, http.StatusBadRequest, "invalid partial search body")
			return
		}
	}

	type match struct {
		id     string
		raw    json.RawMessage
		merged map[string]any
	}
	var matches []match
	for _, id := range org.Keys(idx.collection) {
		raw, ok := org.Get(idx.collection, id)
		if !ok {
			continue
		}
		var doc map[string]any
		if json.Unmarshal(raw, &doc) != nil {
			continue
		}
		searchable := doc
		if idx.mergeAttrs {
			searchable = nodeSearchDoc(doc)
		}
		if query.Matches(search.Flatten(searchable)) {
			matches = append(matches, match{id: id, raw: raw, merged: searchable})
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].id < matches[j].id })

	start := queryInt(r, "start", 0)
	rows := queryInt(r, "rows", defaultSearchRows)
	total := len(matches)
	window := matches[clamp(start, 0, total):clamp(start+rows, 0, total)]

	out := make([]any, 0, len(window))
	for _, m := range window {
		if partial != nil {
			data := map[string]any{}
			for key, path := range partial {
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
