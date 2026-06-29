// Package backendtest is the shared conformance suite for store.Backend
// implementations. Each backend package calls Run from its own test, so the two
// backends are held to one identical behavioral contract.
package backendtest

import (
	"bytes"
	"testing"

	"github.com/tas50/cinc-zero/internal/store"
)

// Run exercises the full Backend contract against a fresh backend produced by
// newBackend for each sub-test.
func Run(t *testing.T, newBackend func(t *testing.T) store.Backend) {
	t.Helper()
	t.Run("ObjectRoundTrip", func(t *testing.T) { testObjectRoundTrip(t, newBackend(t)) })
	t.Run("CopyIndependence", func(t *testing.T) { testCopyIndependence(t, newBackend(t)) })
	t.Run("CreateConflict", func(t *testing.T) { testCreateConflict(t, newBackend(t)) })
	t.Run("DeleteSemantics", func(t *testing.T) { testDeleteSemantics(t, newBackend(t)) })
	t.Run("KeysSorted", func(t *testing.T) { testKeysSorted(t, newBackend(t)) })
	t.Run("CollectionsSorted", func(t *testing.T) { testCollectionsSorted(t, newBackend(t)) })
	t.Run("RangeEarlyStop", func(t *testing.T) { testRangeEarlyStop(t, newBackend(t)) })
	t.Run("OrgNamespacing", func(t *testing.T) { testOrgNamespacing(t, newBackend(t)) })
	t.Run("Blobs", func(t *testing.T) { testBlobs(t, newBackend(t)) })
	t.Run("OrgLifecycle", func(t *testing.T) { testOrgLifecycle(t, newBackend(t)) })
	t.Run("DeleteOrgDropsData", func(t *testing.T) { testDeleteOrgDropsData(t, newBackend(t)) })
}

func mustPut(t *testing.T, b store.Backend, org, coll, key, val string) {
	t.Helper()
	if err := b.Put(org, coll, key, []byte(val)); err != nil {
		t.Fatalf("Put(%q,%q,%q): %v", org, coll, key, err)
	}
}

func testObjectRoundTrip(t *testing.T, b store.Backend) {
	if _, ok, err := b.Get("acme", "nodes", "web"); err != nil || ok {
		t.Fatalf("missing Get: ok=%v err=%v", ok, err)
	}
	mustPut(t, b, "acme", "nodes", "web", `{"name":"web"}`)
	got, ok, err := b.Get("acme", "nodes", "web")
	if err != nil || !ok || string(got) != `{"name":"web"}` {
		t.Fatalf("Get after Put: got=%q ok=%v err=%v", got, ok, err)
	}
}

func testCopyIndependence(t *testing.T, b store.Backend) {
	mustPut(t, b, "acme", "nodes", "web", `{"a":1}`)
	got, _, _ := b.Get("acme", "nodes", "web")
	got[0] = 'X' // mutating the returned slice must not affect stored state
	again, _, _ := b.Get("acme", "nodes", "web")
	if string(again) != `{"a":1}` {
		t.Fatalf("stored value mutated via returned slice: %q", again)
	}
}

func testCreateConflict(t *testing.T, b store.Backend) {
	if err := b.Create("acme", "roles", "base", []byte(`{}`)); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if err := b.Create("acme", "roles", "base", []byte(`{}`)); err != store.ErrConflict {
		t.Fatalf("second Create: want ErrConflict, got %v", err)
	}
}

func testDeleteSemantics(t *testing.T, b store.Backend) {
	if _, existed, err := b.Delete("acme", "nodes", "ghost"); err != nil || existed {
		t.Fatalf("delete missing: existed=%v err=%v", existed, err)
	}
	mustPut(t, b, "acme", "nodes", "web", `{"name":"web"}`)
	old, existed, err := b.Delete("acme", "nodes", "web")
	if err != nil || !existed || string(old) != `{"name":"web"}` {
		t.Fatalf("delete existing: old=%q existed=%v err=%v", old, existed, err)
	}
	if _, ok, _ := b.Get("acme", "nodes", "web"); ok {
		t.Fatal("value present after delete")
	}
}

func testKeysSorted(t *testing.T, b store.Backend) {
	mustPut(t, b, "acme", "nodes", "c", `{}`)
	mustPut(t, b, "acme", "nodes", "a", `{}`)
	mustPut(t, b, "acme", "nodes", "b", `{}`)
	keys, err := b.Keys("acme", "nodes")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 || keys[0] != "a" || keys[1] != "b" || keys[2] != "c" {
		t.Fatalf("Keys not sorted: %v", keys)
	}
	empty, err := b.Keys("acme", "missing")
	if err != nil || len(empty) != 0 {
		t.Fatalf("Keys of empty coll: %v err=%v", empty, err)
	}
}

func testCollectionsSorted(t *testing.T, b store.Backend) {
	mustPut(t, b, "acme", "roles", "x", `{}`)
	mustPut(t, b, "acme", "nodes", "x", `{}`)
	// A collection emptied by Delete must not be reported.
	mustPut(t, b, "acme", "envs", "x", `{}`)
	b.Delete("acme", "envs", "x")
	colls, err := b.Collections("acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(colls) != 2 || colls[0] != "nodes" || colls[1] != "roles" {
		t.Fatalf("Collections: %v", colls)
	}
}

func testRangeEarlyStop(t *testing.T, b store.Backend) {
	for _, k := range []string{"a", "b", "c", "d"} {
		mustPut(t, b, "acme", "nodes", k, `{}`)
	}
	seen := 0
	err := b.Range("acme", "nodes", func(key string, raw []byte) bool {
		seen++
		return seen < 2 // stop after the second
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen != 2 {
		t.Fatalf("Range did not stop early: visited %d", seen)
	}
}

func testOrgNamespacing(t *testing.T, b store.Backend) {
	mustPut(t, b, "acme", "nodes", "web", `{"org":"acme"}`)
	mustPut(t, b, "other", "nodes", "web", `{"org":"other"}`)
	mustPut(t, b, "", "users", "pivotal", `{"global":true}`)
	a, _, _ := b.Get("acme", "nodes", "web")
	o, _, _ := b.Get("other", "nodes", "web")
	g, _, _ := b.Get("", "users", "pivotal")
	if string(a) != `{"org":"acme"}` || string(o) != `{"org":"other"}` || string(g) != `{"global":true}` {
		t.Fatalf("org namespaces leaked: a=%q o=%q g=%q", a, o, g)
	}
}

func testBlobs(t *testing.T, b store.Backend) {
	if ok, err := b.HasBlob("acme", "deadbeef"); err != nil || ok {
		t.Fatalf("HasBlob missing: ok=%v err=%v", ok, err)
	}
	if err := b.PutBlob("acme", "deadbeef", []byte("filecontent")); err != nil {
		t.Fatal(err)
	}
	got, ok, err := b.Blob("acme", "deadbeef")
	if err != nil || !ok || string(got) != "filecontent" {
		t.Fatalf("Blob: got=%q ok=%v err=%v", got, ok, err)
	}
	got[0] = 'X'
	again, _, _ := b.Blob("acme", "deadbeef")
	if !bytes.Equal(again, []byte("filecontent")) {
		t.Fatalf("blob mutated via returned slice: %q", again)
	}
	if err := b.DeleteBlob("acme", "deadbeef"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := b.HasBlob("acme", "deadbeef"); ok {
		t.Fatal("blob present after delete")
	}
}

func testOrgLifecycle(t *testing.T, b store.Backend) {
	if err := b.CreateOrg("acme"); err != nil {
		t.Fatal(err)
	}
	if err := b.CreateOrg("acme"); err != store.ErrConflict {
		t.Fatalf("CreateOrg conflict: want ErrConflict, got %v", err)
	}
	if err := b.CreateOrg("beta"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := b.HasOrg("acme"); !ok {
		t.Fatal("HasOrg acme should be true")
	}
	if ok, _ := b.HasOrg("nope"); ok {
		t.Fatal("HasOrg nope should be false")
	}
	orgs, err := b.ListOrgs()
	if err != nil || len(orgs) != 2 || orgs[0] != "acme" || orgs[1] != "beta" {
		t.Fatalf("ListOrgs: %v err=%v", orgs, err)
	}
	if existed, _ := b.DeleteOrg("acme"); !existed {
		t.Fatal("DeleteOrg acme should report existed")
	}
	if existed, _ := b.DeleteOrg("acme"); existed {
		t.Fatal("second DeleteOrg acme should report not-existed")
	}
}

func testDeleteOrgDropsData(t *testing.T, b store.Backend) {
	if err := b.CreateOrg("acme"); err != nil {
		t.Fatal(err)
	}
	mustPut(t, b, "acme", "nodes", "web", `{}`)
	if err := b.PutBlob("acme", "cafe", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.DeleteOrg("acme"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := b.Get("acme", "nodes", "web"); ok {
		t.Fatal("object survived DeleteOrg")
	}
	if ok, _ := b.HasBlob("acme", "cafe"); ok {
		t.Fatal("blob survived DeleteOrg")
	}
}
