package server

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// signedBody performs a signed request and returns the status and body.
func signedBody(t *testing.T, req *http.Request) (int, []byte) {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp.StatusCode, body
}

// TestCreatorOwnsObjectOthersDenied verifies the creator-grant: the client that
// creates a node can read/update/delete it, while a different client cannot —
// mirroring Chef, where the creator is granted full control of what it created.
func TestCreatorOwnsObjectOthersDenied(t *testing.T) {
	srv := startServer(t, Options{Orgs: []string{"acme"}, EnforceACL: true})
	base := srv.URL() + "/organizations/acme"

	k1 := createClientAndKey(t, srv, base, "node1")
	k2 := createClientAndKey(t, srv, base, "node2")

	// node1 creates and fully manages its own node.
	mustStatus(t, signedAs(t, "node1", k1, "POST", base+"/nodes", `{"name":"node1"}`), 201)
	mustStatus(t, signedAs(t, "node1", k1, "PUT", base+"/nodes/node1", `{"name":"node1","normal":{"a":1}}`), 200)

	// node2 (a different client) cannot read, update, or delete node1's node.
	mustStatus(t, signedAs(t, "node2", k2, "PUT", base+"/nodes/node1", `{"name":"node1"}`), 403)
	mustStatus(t, signedAs(t, "node2", k2, "DELETE", base+"/nodes/node1", ""), 403)

	// node1 may delete its own node.
	mustStatus(t, signedAs(t, "node1", k1, "DELETE", base+"/nodes/node1", ""), 200)
}

// TestRegisteredClientJoinsClientsGroup verifies a newly registered client is a
// member of the org's "clients" group, while the validator (a restricted client)
// is not.
func TestRegisteredClientJoinsClientsGroup(t *testing.T) {
	srv := startServer(t, Options{Orgs: []string{"acme"}, EnforceACL: true})
	base := srv.URL() + "/organizations/acme"
	createClientAndKey(t, srv, base, "node1")

	code, body := signedBody(t, signed(t, srv, "GET", base+"/groups/clients", ""))
	if code != 200 {
		t.Fatalf("read clients group = %d: %s", code, body)
	}
	var g struct {
		Clients []string `json:"clients"`
	}
	if err := json.Unmarshal(body, &g); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(g.Clients, "node1") {
		t.Errorf("registered client node1 not in clients group: %v", g.Clients)
	}
	if slices.Contains(g.Clients, "acme-validator") {
		t.Errorf("validator should not be in the clients group: %v", g.Clients)
	}
}

// TestClientsCanCreateNodesNotRoles verifies the grant is targeted as in Chef:
// the clients group can create nodes (self-registration) but not roles.
func TestClientsCanCreateNodesNotRoles(t *testing.T) {
	srv := startServer(t, Options{Orgs: []string{"acme"}, EnforceACL: true})
	base := srv.URL() + "/organizations/acme"
	key := createClientAndKey(t, srv, base, "node1")

	mustStatus(t, signedAs(t, "node1", key, "POST", base+"/nodes", `{"name":"node1"}`), 201)
	mustStatus(t, signedAs(t, "node1", key, "POST", base+"/roles", `{"name":"web"}`), 403)
}

// TestPermissiveModeWritesNoCreatorACL verifies the permissive default is
// unchanged: with enforcement off, creating an object writes no per-object ACL,
// so the object's ACL is still the default (no creator actor recorded).
func TestPermissiveModeWritesNoCreatorACL(t *testing.T) {
	srv := startServer(t, Options{Orgs: []string{"acme"}, DisableAuth: true}) // enforcement off
	base := srv.URL() + "/organizations/acme"

	resp, err := http.Post(base+"/nodes", "application/json", strings.NewReader(`{"name":"n1"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("create node = %d", resp.StatusCode)
	}

	aclBody := getBody(t, base+"/nodes/n1/_acl")
	var acl map[string]struct {
		Actors []string `json:"actors"`
	}
	if err := json.Unmarshal([]byte(aclBody), &acl); err != nil {
		t.Fatal(err)
	}
	for perm, ace := range acl {
		if len(ace.Actors) != 0 {
			t.Errorf("permissive mode recorded a creator on %q: %v", perm, ace.Actors)
		}
	}
}
