// Package search implements Chef's in-process search: it flattens objects into
// the field map Chef's indexer produces and evaluates Solr-style queries
// against that map, matching how chef-zero serves search without an external
// search engine.
package search

import (
	"strconv"
	"strings"
)

// Flatten produces Chef's searchable field map for a document. Every leaf value
// is indexed under each suffix of its underscore-joined key path, so a nested
// attribute foo.bar.baz can be matched as "foo_bar_baz", "bar_baz", or "baz".
// Array elements are indexed under the same key. Keys and values are lowercased
// to match Solr's default text analysis (so queries are case-insensitive).
func Flatten(doc map[string]any) map[string][]string {
	fields := map[string][]string{}
	add := func(path []string, val string) {
		val = strings.ToLower(val)
		for i := range path {
			key := strings.ToLower(strings.Join(path[i:], "_"))
			fields[key] = append(fields[key], val)
		}
	}
	var walk func(path []string, v any)
	walk = func(path []string, v any) {
		switch t := v.(type) {
		case map[string]any:
			for k, vv := range t {
				child := append(append([]string{}, path...), k)
				walk(child, vv)
			}
		case []any:
			for _, vv := range t {
				walk(path, vv)
			}
		case nil:
			if len(path) > 0 {
				add(path, "")
			}
		default:
			if len(path) > 0 {
				add(path, scalarString(t))
			}
		}
	}
	walk(nil, doc)
	return fields
}

// scalarString renders a JSON scalar as its searchable string form. JSON
// numbers decode as float64; integers are rendered without a trailing ".0".
func scalarString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	default:
		return ""
	}
}
