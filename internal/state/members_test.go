package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tas50/cinc-zero/internal/api"
	"github.com/tas50/cinc-zero/internal/store"
)

// members.json associates its usernames with the org, in any accepted shape.
func TestLoadMembersAssociatesUsers(t *testing.T) {
	dir := t.TempDir()
	orgDir := filepath.Join(dir, "organizations", "acme")
	if err := os.MkdirAll(orgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orgDir, "members.json"),
		[]byte(`[{"user":{"username":"anna"}}, "ben", {"username":"cara"}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	st := store.New()
	if _, err := Load(st, dir); err != nil {
		t.Fatalf("load: %v", err)
	}
	org, ok, err := st.Org("acme")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("acme not created")
	}
	for _, name := range []string{"anna", "ben", "cara"} {
		_, ok, err := org.Get(api.AssociationUsersCollection, name)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Errorf("%s not associated with acme", name)
		}
	}
}

// The committed seed associates anna and ben with acme.
func TestSeedAssociatesAnna(t *testing.T) {
	_, org, _ := loadSeed(t)
	_, ok, err := org.Get(api.AssociationUsersCollection, "anna")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("anna is not a member of acme in the seed")
	}
}
