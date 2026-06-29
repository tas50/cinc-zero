package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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

// loadMembers also adds each member to the org "users" group, mirroring
// associateUser (POST /organizations/<org>/users). Membership in "users" is
// what lets a regular member inherit the default read ACL under enforcement;
// without it a console-logged-in user is denied everything.
func TestLoadMembersJoinsUsersGroup(t *testing.T) {
	dir := t.TempDir()
	orgDir := filepath.Join(dir, "organizations", "acme")
	if err := os.MkdirAll(orgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orgDir, "members.json"),
		[]byte(`["dana", {"username":"erin"}]`), 0o644); err != nil {
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

	raw, ok, err := org.Get("groups", "users")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("users group missing")
	}
	var g struct {
		Users []string `json:"users"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"dana", "erin"} {
		if !slices.Contains(g.Users, name) {
			t.Errorf("%s is not in the users group: %v", name, g.Users)
		}
	}
}

// The committed seed associates tim and jack with acme.
func TestSeedAssociatesTim(t *testing.T) {
	_, org, _ := loadSeed(t)
	_, ok, err := org.Get(api.AssociationUsersCollection, "tim")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("tim is not a member of acme in the seed")
	}
}

// The committed seed makes tim a full org admin (in the acme "admins" group)
// while jack is a regular member; loadMembers puts both in the "users" group.
// This is the fixture equivalent of `chef-server-ctl org-user-add acme tim
// --admin`.
func TestSeedUserGroupMembership(t *testing.T) {
	_, org, _ := loadSeed(t)

	groupUsers := func(group string) []string {
		raw, ok, err := org.Get("groups", group)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatalf("group %q missing", group)
		}
		var g struct {
			Users []string `json:"users"`
		}
		if err := json.Unmarshal(raw, &g); err != nil {
			t.Fatal(err)
		}
		return g.Users
	}

	if admins := groupUsers("admins"); !slices.Contains(admins, "tim") {
		t.Errorf("tim should be an org admin (in admins group): %v", admins)
	} else if slices.Contains(admins, "jack") {
		t.Errorf("jack should not be an admin: %v", admins)
	}

	if users := groupUsers("users"); !slices.Contains(users, "tim") || !slices.Contains(users, "jack") {
		t.Errorf("tim and jack should both be in the users group: %v", users)
	}
}
