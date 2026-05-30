package store

import (
	"fmt"
	"sync"
	"testing"
)

func TestCreateOrgAndLookup(t *testing.T) {
	s := New()
	if _, ok := s.Org("acme"); ok {
		t.Fatal("org should not exist yet")
	}
	org, err := s.CreateOrg("acme")
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	if org == nil {
		t.Fatal("expected org")
	}
	got, ok := s.Org("acme")
	if !ok || got != org {
		t.Fatal("Org lookup did not return created org")
	}
}

func TestCreateOrgConflict(t *testing.T) {
	s := New()
	if _, err := s.CreateOrg("acme"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateOrg("acme"); err != ErrConflict {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestListAndDeleteOrgs(t *testing.T) {
	s := New()
	s.CreateOrg("b")
	s.CreateOrg("a")
	got := s.ListOrgs()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("ListOrgs not sorted: %v", got)
	}
	if !s.DeleteOrg("a") {
		t.Fatal("DeleteOrg should return true")
	}
	if s.DeleteOrg("a") {
		t.Fatal("second DeleteOrg should return false")
	}
	if len(s.ListOrgs()) != 1 {
		t.Fatal("org not deleted")
	}
}

func TestOrgCreateGetDelete(t *testing.T) {
	s := New()
	org, _ := s.CreateOrg("acme")

	if _, ok := org.Get("nodes", "web"); ok {
		t.Fatal("node should not exist")
	}
	if err := org.Create("nodes", "web", []byte(`{"name":"web"}`)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := org.Create("nodes", "web", []byte(`{}`)); err != ErrConflict {
		t.Fatalf("expected ErrConflict on duplicate, got %v", err)
	}
	got, ok := org.Get("nodes", "web")
	if !ok || string(got) != `{"name":"web"}` {
		t.Fatalf("Get returned %q ok=%v", got, ok)
	}
	deleted, ok := org.Delete("nodes", "web")
	if !ok || string(deleted) != `{"name":"web"}` {
		t.Fatalf("Delete returned %q ok=%v", deleted, ok)
	}
	if _, ok := org.Get("nodes", "web"); ok {
		t.Fatal("node should be gone after delete")
	}
}

func TestOrgPutUpsertsAndKeysSorted(t *testing.T) {
	s := New()
	org, _ := s.CreateOrg("acme")
	org.Put("roles", "z", []byte(`{"v":1}`))
	org.Put("roles", "a", []byte(`{"v":1}`))
	org.Put("roles", "a", []byte(`{"v":2}`)) // upsert, no error
	keys := org.Keys("roles")
	if len(keys) != 2 || keys[0] != "a" || keys[1] != "z" {
		t.Fatalf("Keys not sorted unique: %v", keys)
	}
	got, _ := org.Get("roles", "a")
	if string(got) != `{"v":2}` {
		t.Fatalf("Put did not upsert: %s", got)
	}
}

func TestStoredValueIsCopied(t *testing.T) {
	s := New()
	org, _ := s.CreateOrg("acme")
	val := []byte(`{"name":"web"}`)
	org.Put("nodes", "web", val)
	val[2] = 'X' // mutate caller's slice
	got, _ := org.Get("nodes", "web")
	if string(got) != `{"name":"web"}` {
		t.Fatalf("store aliased caller slice: %s", got)
	}
}

func TestCollectionsListsNonEmptyOnly(t *testing.T) {
	s := New()
	org, _ := s.CreateOrg("acme")
	if got := org.Name(); got != "acme" {
		t.Fatalf("Name = %q", got)
	}
	if cols := org.Collections(); len(cols) != 0 {
		t.Fatalf("expected no collections, got %v", cols)
	}
	org.Put("roles", "web", []byte(`{}`))
	org.Put("nodes", "n1", []byte(`{}`))
	org.Delete("nodes", "n1") // empties the collection
	cols := org.Collections()
	if len(cols) != 1 || cols[0] != "roles" {
		t.Fatalf("Collections should list only non-empty: %v", cols)
	}
}

// TestConcurrentAccess exercises the locks; run with -race.
func TestConcurrentAccess(t *testing.T) {
	s := New()
	org, _ := s.CreateOrg("acme")
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("n%d", i%10)
			org.Put("nodes", key, []byte(`{"i":1}`))
			org.Get("nodes", key)
			org.Keys("nodes")
			s.ListOrgs()
		}(i)
	}
	wg.Wait()
}

func TestGlobalSpaceIsStableAndSeparate(t *testing.T) {
	s := New()
	g := s.Global()
	if g == nil {
		t.Fatal("Global returned nil")
	}
	if s.Global() != g {
		t.Fatal("Global should return the same instance")
	}
	g.Put("users", "admin", []byte(`{"name":"admin"}`))
	org, _ := s.CreateOrg("acme")
	if _, ok := org.Get("users", "admin"); ok {
		t.Fatal("global users must not leak into orgs")
	}
	if got, ok := g.Get("users", "admin"); !ok || string(got) != `{"name":"admin"}` {
		t.Fatal("global value not retrievable")
	}
}

func TestOrgsAreIsolated(t *testing.T) {
	s := New()
	a, _ := s.CreateOrg("a")
	b, _ := s.CreateOrg("b")
	a.Put("nodes", "web", []byte(`{}`))
	if _, ok := b.Get("nodes", "web"); ok {
		t.Fatal("orgs must not share data")
	}
}
