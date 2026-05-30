package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tas50/cinc-zero/internal/store"
)

// writeRepo lays out a small chef-repo under dir.
func writeRepo(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	writeRepo(t, dir, map[string]string{
		"nodes/web01.json":           `{"name":"web01","chef_environment":"prod"}`,
		"roles/web.json":             `{"name":"web","run_list":["recipe[nginx]"]}`,
		"environments/prod.json":     `{"name":"prod"}`,
		"clients/builder.json":       `{"name":"builder"}`,
		"policies/base.json":         `{"name":"base"}`,
		"data_bags/users/alice.json": `{"id":"alice","admin":true}`,
		"data_bags/users/bob.json":   `{"id":"bob"}`,
		"data_bags/secrets/key.json": `{"id":"key"}`,
		"unrelated/ignore.txt":       `not json, not in a known dir`,
	})

	st := store.New()
	org, _ := st.CreateOrg("acme")
	sum, err := Load(org, dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if _, ok := org.Get("nodes", "web01"); !ok {
		t.Fatal("node web01 not loaded")
	}
	if _, ok := org.Get("roles", "web"); !ok {
		t.Fatal("role web not loaded")
	}
	if _, ok := org.Get("environments", "prod"); !ok {
		t.Fatal("environment prod not loaded")
	}
	if _, ok := org.Get("clients", "builder"); !ok {
		t.Fatal("client builder not loaded")
	}
	if _, ok := org.Get("policies", "base"); !ok {
		t.Fatal("policy base not loaded")
	}
	// Data bags register the bag and store items in the bag's item collection.
	if _, ok := org.Get("data_bags", "users"); !ok {
		t.Fatal("data bag users not registered")
	}
	if _, ok := org.Get("databag_items:users", "alice"); !ok {
		t.Fatal("data bag item alice not loaded")
	}
	if _, ok := org.Get("databag_items:secrets", "key"); !ok {
		t.Fatal("data bag item key not loaded")
	}

	if sum.Counts["nodes"] != 1 || sum.Counts["data_bag_items"] != 3 {
		t.Fatalf("summary counts = %+v", sum.Counts)
	}
}

func TestLoadEmptyOrMissing(t *testing.T) {
	st := store.New()
	org, _ := st.CreateOrg("acme")

	// A non-existent repo path is not an error; it just loads nothing.
	sum, err := Load(org, filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("Load(missing): %v", err)
	}
	if len(sum.Counts) != 0 {
		t.Fatalf("expected no counts, got %+v", sum.Counts)
	}
}

func TestLoadNameFallsBackToFilename(t *testing.T) {
	dir := t.TempDir()
	// No "name" field: the filename (sans .json) is used as the key.
	writeRepo(t, dir, map[string]string{"nodes/fromfile.json": `{"chef_environment":"prod"}`})

	st := store.New()
	org, _ := st.CreateOrg("acme")
	if _, err := Load(org, dir); err != nil {
		t.Fatal(err)
	}
	if _, ok := org.Get("nodes", "fromfile"); !ok {
		t.Fatal("node keyed by filename not loaded")
	}
}
