package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tas50/cinc-zero/internal/store"
)

// testOrg returns a fresh, seeded org "acme" for authorization unit tests.
func testOrg(t *testing.T) *store.Org {
	t.Helper()
	st := store.New()
	org, err := st.CreateOrg("acme")
	if err != nil {
		t.Fatal(err)
	}
	SeedOrg(org)
	return org
}

// putGroup writes a group document with the given members.
func putGroup(org *store.Org, name string, users, clients, groups []string) {
	org.Put("groups", name, mustEncode(groupDoc(name, users, clients, groups)))
}

func TestActorGroupsDirectMembership(t *testing.T) {
	org := testOrg(t)
	putGroup(org, "users", []string{"alice"}, nil, nil)

	got := actorGroups(org, Actor{Name: "alice"})
	if !got["users"] {
		t.Fatalf("alice should be in users group; got %v", got)
	}
	if got["admins"] {
		t.Fatalf("alice should not be in admins; got %v", got)
	}
}

func TestActorGroupsDistinguishesUsersFromClients(t *testing.T) {
	org := testOrg(t)
	// "bob" is listed only as a client, not a user.
	putGroup(org, "clients", nil, []string{"bob"}, nil)

	asClient := actorGroups(org, Actor{Name: "bob", IsClient: true})
	if !asClient["clients"] {
		t.Fatalf("client bob should be in clients group; got %v", asClient)
	}
	asUser := actorGroups(org, Actor{Name: "bob", IsClient: false})
	if asUser["clients"] {
		t.Fatalf("user bob should not match the client membership; got %v", asUser)
	}
}

func TestActorGroupsTransitive(t *testing.T) {
	org := testOrg(t)
	putGroup(org, "users", []string{"alice"}, nil, nil)
	// superusers contains the users group; alice is therefore a superuser too.
	putGroup(org, "superusers", nil, nil, []string{"users"})

	got := actorGroups(org, Actor{Name: "alice"})
	if !got["users"] || !got["superusers"] {
		t.Fatalf("alice should be in users and superusers; got %v", got)
	}
}

func TestActorGroupsCycleTerminates(t *testing.T) {
	org := testOrg(t)
	putGroup(org, "a", []string{"alice"}, nil, []string{"b"})
	putGroup(org, "b", nil, nil, []string{"a"})

	got := actorGroups(org, Actor{Name: "alice"})
	if !got["a"] || !got["b"] {
		t.Fatalf("alice should be in both cyclic groups; got %v", got)
	}
}

func TestActorAllowedDirectActor(t *testing.T) {
	org := testOrg(t)
	org.Put("acls", aclKey("nodes", "web01"), mustEncode(map[string]any{
		"read": map[string]any{"actors": []string{"alice"}, "groups": []string{}},
	}))
	if !actorAllowed(org, Actor{Name: "alice"}, "nodes", "web01", "read") {
		t.Fatal("alice is a direct actor on read and should be allowed")
	}
	if actorAllowed(org, Actor{Name: "mallory"}, "nodes", "web01", "read") {
		t.Fatal("mallory is not on the ACE and should be denied")
	}
}

func TestActorAllowedViaGroupDefaultACL(t *testing.T) {
	org := testOrg(t)
	// Default ACL grants read to admins,users,clients. Put alice in users.
	putGroup(org, "users", []string{"alice"}, nil, nil)

	if !actorAllowed(org, Actor{Name: "alice"}, "nodes", "web01", "read") {
		t.Fatal("alice (in users) should get default read")
	}
	// A non-member is denied even though the object has the permissive default.
	if actorAllowed(org, Actor{Name: "stranger"}, "nodes", "web01", "read") {
		t.Fatal("stranger is in no group and should be denied")
	}
	// Default ACL does not grant create to clients.
	putGroup(org, "clients", nil, []string{"node1"}, nil)
	if actorAllowed(org, Actor{Name: "node1", IsClient: true}, "nodes", "web01", "create") {
		t.Fatal("clients are not granted create by the default ACL")
	}
}

func TestClassifyRequest(t *testing.T) {
	cases := []struct {
		method, path string
		want         *authzCheck // nil means "unclassified / allow-through"
	}{
		// Generic object collection: container ACL governs list/create.
		{"GET", "/organizations/acme/nodes", &authzCheck{aclType: "containers", aclName: "nodes", perm: "read"}},
		{"POST", "/organizations/acme/nodes", &authzCheck{aclType: "containers", aclName: "nodes", perm: "create"}},
		// Generic object item: object ACL governs, existence checked first.
		{"GET", "/organizations/acme/nodes/web01", &authzCheck{aclType: "nodes", aclName: "web01", perm: "read", existColl: "nodes", existKey: "web01", existMsg: "Cannot find nodes web01"}},
		{"HEAD", "/organizations/acme/nodes/web01", &authzCheck{aclType: "nodes", aclName: "web01", perm: "read", existColl: "nodes", existKey: "web01", existMsg: "Cannot find nodes web01"}},
		{"PUT", "/organizations/acme/roles/web", &authzCheck{aclType: "roles", aclName: "web", perm: "update", existColl: "roles", existKey: "web", existMsg: "Cannot find roles web"}},
		{"DELETE", "/organizations/acme/environments/prod", &authzCheck{aclType: "environments", aclName: "prod", perm: "delete", existColl: "environments", existKey: "prod", existMsg: "Cannot find environments prod"}},
		// _acl endpoints require grant on the target object; no existence check.
		{"GET", "/organizations/acme/nodes/web01/_acl", &authzCheck{aclType: "nodes", aclName: "web01", perm: "grant"}},
		{"PUT", "/organizations/acme/nodes/web01/_acl/grant", &authzCheck{aclType: "nodes", aclName: "web01", perm: "grant"}},
		{"GET", "/organizations/acme/_acl", &authzCheck{aclType: "organizations", aclName: "acme", perm: "grant"}},
		// Organization read.
		{"GET", "/organizations/acme", &authzCheck{aclType: "organizations", aclName: "acme", perm: "read"}},
		// Data bags: container governs the collection; the bag ACL governs items.
		{"GET", "/organizations/acme/data", &authzCheck{aclType: "containers", aclName: "data", perm: "read"}},
		{"POST", "/organizations/acme/data", &authzCheck{aclType: "containers", aclName: "data", perm: "create"}},
		{"GET", "/organizations/acme/data/secrets", &authzCheck{aclType: "data", aclName: "secrets", perm: "read", existColl: "data_bags", existKey: "secrets", existMsg: "Cannot find data bag secrets"}},
		{"POST", "/organizations/acme/data/secrets", &authzCheck{aclType: "data", aclName: "secrets", perm: "update", existColl: "data_bags", existKey: "secrets", existMsg: "Cannot find data bag secrets"}},
		{"DELETE", "/organizations/acme/data/secrets", &authzCheck{aclType: "data", aclName: "secrets", perm: "delete", existColl: "data_bags", existKey: "secrets", existMsg: "Cannot find data bag secrets"}},
		{"PUT", "/organizations/acme/data/secrets/item1", &authzCheck{aclType: "data", aclName: "secrets", perm: "update", existColl: "data_bags", existKey: "secrets", existMsg: "Cannot find data bag secrets"}},
		// Cookbooks: versioned; the cookbook object ACL governs versions.
		{"GET", "/organizations/acme/cookbooks", &authzCheck{aclType: "containers", aclName: "cookbooks", perm: "read"}},
		{"GET", "/organizations/acme/cookbooks/apache2", &authzCheck{aclType: "cookbooks", aclName: "apache2", perm: "read"}},
		{"GET", "/organizations/acme/cookbooks/apache2/1.0.0", &authzCheck{aclType: "cookbooks", aclName: "apache2", perm: "read"}},
		{"PUT", "/organizations/acme/cookbooks/apache2/1.0.0", &authzCheck{aclType: "cookbooks", aclName: "apache2", perm: "update"}},
		{"DELETE", "/organizations/acme/cookbooks/apache2/1.0.0", &authzCheck{aclType: "cookbooks", aclName: "apache2", perm: "delete"}},
		// Cookbook pseudo-endpoints fall back to the container.
		{"GET", "/organizations/acme/cookbooks/_latest", &authzCheck{aclType: "containers", aclName: "cookbooks", perm: "read"}},
		{"PUT", "/organizations/acme/cookbook_artifacts/foo/abc123", &authzCheck{aclType: "cookbook_artifacts", aclName: "foo", perm: "update"}},
		// Unclassified: allow-through (no ACL restrictions or out of scope).
		{"GET", "/organizations/acme/search/node", nil},
		{"GET", "/organizations/acme/sandboxes", nil},
		{"POST", "/organizations/acme/users", nil}, // org association
		{"GET", "/users", nil},
		{"POST", "/organizations", nil},
		{"GET", "/organizations", nil},
		{"GET", "/organizations/acme/policies/app/revisions/1.0.0", nil}, // sub-resource
	}
	for _, c := range cases {
		got, ok := classifyRequest(c.method, c.path)
		if c.want == nil {
			if ok {
				t.Errorf("%s %s: classified %+v, want allow-through", c.method, c.path, got)
			}
			continue
		}
		if !ok {
			t.Errorf("%s %s: allow-through, want %+v", c.method, c.path, *c.want)
			continue
		}
		if *got != *c.want {
			t.Errorf("%s %s:\n got %+v\nwant %+v", c.method, c.path, *got, *c.want)
		}
	}
}

// enforcingHandler builds an API handler with ACL enforcement enabled, backed
// by a store with a seeded org "acme" and a single node "web01".
func enforcingHandler(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	st := store.New()
	org, err := st.CreateOrg("acme")
	if err != nil {
		t.Fatal(err)
	}
	SeedOrg(org)
	org.Put("nodes", "web01", []byte(`{"name":"web01"}`))
	return New(st, WithACLEnforcement(true)).Handler(), st
}

// authzReq sends a request through h as actor and returns status and body.
func authzReq(t *testing.T, h http.Handler, actor Actor, method, target, body string) (int, string) {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	r = r.WithContext(WithActor(r.Context(), actor))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec.Code, rec.Body.String()
}

func TestEnforceDeniesWithoutPermission(t *testing.T) {
	h, _ := enforcingHandler(t)
	// "stranger" is a user in no group, so the default ACL denies read.
	code, body := authzReq(t, h, Actor{Name: "stranger"}, "GET", "/organizations/acme/nodes/web01", "")
	if code != http.StatusForbidden {
		t.Fatalf("denied read = %d, want 403; body %s", code, body)
	}
	var errObj struct {
		Error []string `json:"error"`
	}
	json.Unmarshal([]byte(body), &errObj)
	if len(errObj.Error) == 0 {
		t.Fatalf("403 should carry a Chef error array, got %s", body)
	}
}

func TestEnforceAllowsViaGroup(t *testing.T) {
	h, st := enforcingHandler(t)
	org, _ := st.Org("acme")
	putGroup(org, "users", []string{"alice"}, nil, nil)
	code, body := authzReq(t, h, Actor{Name: "alice"}, "GET", "/organizations/acme/nodes/web01", "")
	if code != http.StatusOK {
		t.Fatalf("alice (in users) read = %d, want 200; body %s", code, body)
	}
}

func TestEnforceSuperuserBypass(t *testing.T) {
	h, _ := enforcingHandler(t)
	// A global admin belongs to no group but bypasses ACL checks (pivotal).
	code, _ := authzReq(t, h, Actor{Name: "pivotal", IsGlobalAdmin: true}, "GET", "/organizations/acme/nodes/web01", "")
	if code != http.StatusOK {
		t.Fatalf("superuser read = %d, want 200", code)
	}
}

func TestEnforceExistenceBeforeAuthz(t *testing.T) {
	h, _ := enforcingHandler(t)
	// A missing object reports 404 even though the actor also lacks read — the
	// existence check precedes authorization.
	code, _ := authzReq(t, h, Actor{Name: "stranger"}, "GET", "/organizations/acme/nodes/ghost", "")
	if code != http.StatusNotFound {
		t.Fatalf("missing object = %d, want 404 (existence before authz)", code)
	}
}

func TestEnforceAllowsUnclassifiedRoutes(t *testing.T) {
	h, _ := enforcingHandler(t)
	// Search carries no ACL restriction in Chef; enforcement must not 403 it.
	code, _ := authzReq(t, h, Actor{Name: "stranger"}, "GET", "/organizations/acme/search", "")
	if code == http.StatusForbidden {
		t.Fatalf("search should be allowed through, got 403")
	}
}

func TestEnforceDisabledAllowsEverything(t *testing.T) {
	st := store.New()
	org, _ := st.CreateOrg("acme")
	SeedOrg(org)
	org.Put("nodes", "web01", []byte(`{"name":"web01"}`))
	h := New(st).Handler() // enforcement off (default)
	code, _ := authzReq(t, h, Actor{Name: "stranger"}, "GET", "/organizations/acme/nodes/web01", "")
	if code != http.StatusOK {
		t.Fatalf("enforcement off: stranger read = %d, want 200", code)
	}
}

func TestSeedGrantsValidatorClientCreate(t *testing.T) {
	st := store.New()
	if _, err := CreateOrganization(st, "acme", "acme"); err != nil {
		t.Fatal(err)
	}
	org, _ := st.Org("acme")
	if !actorAllowed(org, Actor{Name: "acme-validator", IsClient: true}, "containers", "clients", "create") {
		t.Fatal("the org validator should be granted create on the clients container")
	}
	// A non-validator client is still denied create.
	if actorAllowed(org, Actor{Name: "node1", IsClient: true}, "containers", "clients", "create") {
		t.Fatal("an ordinary client should not be granted create on clients")
	}
}

func TestEnforceValidatorCreatesClient(t *testing.T) {
	st := store.New()
	if _, err := CreateOrganization(st, "acme", "acme"); err != nil {
		t.Fatal(err)
	}
	h := New(st, WithACLEnforcement(true)).Handler()
	code, body := authzReq(t, h, Actor{Name: "acme-validator", IsClient: true},
		"POST", "/organizations/acme/clients", `{"name":"node1"}`)
	if code != http.StatusCreated {
		t.Fatalf("validator create client = %d, want 201; body %s", code, body)
	}
}
