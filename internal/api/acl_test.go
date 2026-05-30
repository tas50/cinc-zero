package api

import (
	"encoding/json"
	"testing"
)

type aclPerm struct {
	Actors []string `json:"actors"`
	Groups []string `json:"groups"`
}

func TestACLDefaultShape(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/organizations/acme"
	do(t, "PUT", base+"/nodes/web01", `{"name":"web01"}`)

	_, body := do(t, "GET", base+"/nodes/web01/_acl", "")
	var acl map[string]aclPerm
	if err := json.Unmarshal([]byte(body), &acl); err != nil {
		t.Fatalf("decode acl: %v (%s)", err, body)
	}
	for _, perm := range []string{"create", "read", "update", "delete", "grant"} {
		p, ok := acl[perm]
		if !ok {
			t.Fatalf("acl missing %q permission: %s", perm, body)
		}
		if p.Actors == nil || p.Groups == nil {
			t.Fatalf("acl %q must have actors and groups arrays: %s", perm, body)
		}
	}
}

func TestACLGetSinglePermission(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/organizations/acme"
	resp, body := do(t, "GET", base+"/roles/web/_acl/read", "")
	if resp.StatusCode != 200 {
		t.Fatalf("get read perm = %d: %s", resp.StatusCode, body)
	}
	var got map[string]aclPerm
	json.Unmarshal([]byte(body), &got)
	if _, ok := got["read"]; !ok || len(got) != 1 {
		t.Fatalf("single-permission response = %s", body)
	}
}

func TestACLUpdatePermission(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/organizations/acme"

	// Grant a specific actor on the "grant" permission.
	resp, body := do(t, "PUT", base+"/nodes/web01/_acl/grant",
		`{"grant":{"actors":["alice"],"groups":["admins"]}}`)
	if resp.StatusCode != 200 {
		t.Fatalf("put grant = %d: %s", resp.StatusCode, body)
	}

	// The change is reflected in the full ACL.
	_, body = do(t, "GET", base+"/nodes/web01/_acl", "")
	var acl map[string]aclPerm
	json.Unmarshal([]byte(body), &acl)
	if len(acl["grant"].Actors) != 1 || acl["grant"].Actors[0] != "alice" {
		t.Fatalf("grant not updated: %s", body)
	}
	// Other permissions remain at their defaults.
	if len(acl["read"].Groups) == 0 {
		t.Fatalf("read permission lost its defaults: %s", body)
	}
}

func TestACLInvalidPermission404(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/organizations/acme"
	resp, _ := do(t, "GET", base+"/nodes/web01/_acl/bogus", "")
	if resp.StatusCode != 404 {
		t.Fatalf("invalid perm = %d, want 404", resp.StatusCode)
	}
	resp, _ = do(t, "PUT", base+"/nodes/web01/_acl/bogus", `{"bogus":{"actors":[],"groups":[]}}`)
	if resp.StatusCode != 404 {
		t.Fatalf("put invalid perm = %d, want 404", resp.StatusCode)
	}
}

func TestACLDataBagAndOrg(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/organizations/acme"

	// Data bag ACL path resolves alongside /data/{bag}/{item}.
	do(t, "POST", base+"/data", `{"name":"secrets"}`)
	resp, body := do(t, "GET", base+"/data/secrets/_acl", "")
	if resp.StatusCode != 200 {
		t.Fatalf("data bag acl = %d: %s", resp.StatusCode, body)
	}

	// Organization-level ACL.
	resp, body = do(t, "GET", base+"/_acl", "")
	if resp.StatusCode != 200 {
		t.Fatalf("org acl = %d: %s", resp.StatusCode, body)
	}
	var acl map[string]aclPerm
	json.Unmarshal([]byte(body), &acl)
	if _, ok := acl["grant"]; !ok {
		t.Fatalf("org acl missing grant: %s", body)
	}
}
