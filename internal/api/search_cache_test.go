package api

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/tas50/cinc-zero/internal/search"
	"github.com/tas50/cinc-zero/internal/store"
)

// benchSearchOrg seeds n nodes with nested attributes for the search benchmarks.
func benchSearchOrg(b *testing.B, n int) (*API, *store.Org, searchIndex, search.Query) {
	b.Helper()
	st := store.New()
	org, _ := st.CreateOrg("acme")
	a := New(st)
	for i := range n {
		doc := fmt.Sprintf(`{"name":"node%d","chef_environment":"production",`+
			`"normal":{"foo":{"bar":"baz%d"},"tags":["a","b","c"]},`+
			`"automatic":{"os":"linux","memory":{"total":"16gb"},`+
			`"network":{"interfaces":{"eth0":{"addr":"10.0.0.%d"}}}},`+
			`"run_list":["recipe[nginx]","recipe[base]"]}`, i, i, i%256)
		org.Put("nodes", fmt.Sprintf("node%d", i), []byte(doc))
	}
	idx, _ := a.resolveIndex(nil, org, "node")
	q, err := search.Parse("chef_environment:production")
	if err != nil {
		b.Fatal(err)
	}
	return a, org, idx, q
}

// BenchmarkSearchScanCold measures a scan that re-unmarshals and re-flattens
// every document — the cost paid on every query before caching (and on the
// first query of any object after a write).
func BenchmarkSearchScanCold(b *testing.B) {
	a, org, idx, q := benchSearchOrg(b, 200)
	b.ReportAllocs()
	for b.Loop() {
		a.search = newSearchCache() // force a full recompute each iteration
		a.collectMatches(org, idx, q)
	}
}

// BenchmarkSearchScanWarm measures a scan served entirely from the flatten
// cache — the steady-state cost of repeated queries over unchanged objects.
func BenchmarkSearchScanWarm(b *testing.B) {
	a, org, idx, q := benchSearchOrg(b, 200)
	a.collectMatches(org, idx, q) // warm the cache
	b.ReportAllocs()
	for b.Loop() {
		a.collectMatches(org, idx, q)
	}
}

// mapPtr returns the runtime identity of a map, so tests can assert whether two
// results are the same cached instance or freshly recomputed.
func mapPtr(m any) uintptr { return reflect.ValueOf(m).Pointer() }

func TestSearchDocCachesUnchangedContent(t *testing.T) {
	st := store.New()
	org, _ := st.CreateOrg("acme")
	a := New(st)
	org.Put("nodes", "web", []byte(`{"name":"web","normal":{"foo":"bar"}}`))
	raw, _ := org.View("nodes", "web")

	_, f1, ok1 := a.searchDoc("nodes", "web", raw, true)
	_, f2, ok2 := a.searchDoc("nodes", "web", raw, true)
	if !ok1 || !ok2 {
		t.Fatal("searchDoc returned not-ok for a valid node")
	}
	if !slices.Contains(f1["foo"], "bar") {
		t.Fatalf("flatten produced %v for foo, want it to contain bar", f1["foo"])
	}
	// Same backing slice → cache hit → the identical fields map is reused.
	if mapPtr(f1) != mapPtr(f2) {
		t.Fatal("expected the cached fields map to be reused for unchanged content")
	}
}

func TestSearchDocRecomputesAfterContentChange(t *testing.T) {
	st := store.New()
	org, _ := st.CreateOrg("acme")
	a := New(st)
	org.Put("nodes", "web", []byte(`{"name":"web","normal":{"foo":"bar"}}`))
	raw1, _ := org.View("nodes", "web")
	_, f1, _ := a.searchDoc("nodes", "web", raw1, true)

	// Updating the node replaces the stored slice, so the cache must recompute.
	org.Put("nodes", "web", []byte(`{"name":"web","normal":{"foo":"changed"}}`))
	raw2, _ := org.View("nodes", "web")
	_, f2, ok := a.searchDoc("nodes", "web", raw2, true)
	if !ok {
		t.Fatal("searchDoc returned not-ok after update")
	}
	if mapPtr(f1) == mapPtr(f2) {
		t.Fatal("stale cache entry reused after the content changed")
	}
	if !slices.Contains(f2["foo"], "changed") || slices.Contains(f2["foo"], "bar") {
		t.Fatalf("recomputed flatten produced %v for foo, want it to contain changed and not bar", f2["foo"])
	}
}

func TestSearchDocReportsNotOKForInvalidJSON(t *testing.T) {
	st := store.New()
	org, _ := st.CreateOrg("acme")
	a := New(st)
	org.Put("nodes", "broken", []byte(`not json`))
	raw, _ := org.View("nodes", "broken")
	if _, _, ok := a.searchDoc("nodes", "broken", raw, false); ok {
		t.Fatal("searchDoc should report not-ok for undecodable JSON")
	}
}
