package server

import (
	"strings"
	"testing"
)

// TestServerLoadsState proves a --state directory hydrates things --repo
// cannot: a global user, an org's chef-objects, and an authz group, all
// served through the API.
func TestServerLoadsState(t *testing.T) {
	dir := t.TempDir()
	writeRepoFiles(t, dir, map[string]string{
		"users/anna.json":                     `{"username":"anna","display_name":"Anna Example"}`,
		"organizations/acme/nodes/web01.json": `{"name":"web01","chef_environment":"production"}`,
		"organizations/acme/groups/devs.json": `{"groupname":"devs","actors":{"users":["anna"],"groups":[],"clients":[]}}`,
	})

	srv := startServer(t, Options{Orgs: []string{"acme"}, DisableAuth: true, StatePath: dir})

	if body := getBody(t, srv.URL()+"/users/anna"); !strings.Contains(body, "anna") {
		t.Fatalf("GET /users/anna = %s, want the loaded global user", body)
	}
	if body := getBody(t, srv.URL()+"/organizations/acme/nodes/web01"); !strings.Contains(body, "production") {
		t.Fatalf("GET node web01 = %s, want the loaded node", body)
	}
	if body := getBody(t, srv.URL()+"/organizations/acme/groups/devs"); !strings.Contains(body, "devs") {
		t.Fatalf("GET group devs = %s, want the loaded group", body)
	}
}

// TestStateAndRepoMutuallyExclusive proves --state and --repo cannot be
// combined: a --state directory already carries everything --repo would.
func TestStateAndRepoMutuallyExclusive(t *testing.T) {
	_, err := New(Options{Repo: "somerepo", StatePath: "somestate", DisableAuth: true})
	if err == nil {
		t.Fatal("New with both Repo and StatePath set should error")
	}
}
