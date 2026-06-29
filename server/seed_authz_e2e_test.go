package server

import (
	"net/http"
	"testing"
)

// These tests encode the original "can't see objects in cinc-console" bug. A
// user logged into a management console is webui-impersonated: the request is
// signed with the webui key but runs under that user's own ACLs, NOT as the
// pivotal superuser. The committed seed makes tim a full org admin and jack a
// regular member (both in the acme "users" group via membership), so both can
// read the org — while a global user who is not an acme member is denied.
//
// Before the fix, tim/jack were only in the custom "devs" group, which the
// default ACL does not grant, so every read returned 403.

// tim is a full org admin (in the acme "admins" group): he can read every
// resource type and use the grant-only _acl endpoint.
func TestSeedWebUIAdminCanReadEverything(t *testing.T) {
	srv := startServer(t, Options{StatePath: "../dev/test-repo", EnforceACL: true})
	base := srv.URL() + "/organizations/acme"

	for _, path := range []string{
		"/nodes", "/nodes/dev-app-01",
		"/roles", "/roles/base",
		"/environments", "/environments/production",
		"/data", "/cookbooks",
	} {
		if got := statusOf(t, webuiSignedAs(t, srv, "tim", "GET", base+path, "")); got != http.StatusOK {
			t.Errorf("tim GET %s = %d, want 200", path, got)
		}
	}

	// The _acl endpoint requires the grant permission, which the default ACL
	// gives only to admins — so this distinguishes tim's admin status.
	if got := statusOf(t, webuiSignedAs(t, srv, "tim", "GET", base+"/nodes/dev-app-01/_acl", "")); got != http.StatusOK {
		t.Errorf("tim GET node _acl = %d, want 200 (admin holds grant)", got)
	}
}

// jack is a regular member (in "users" only): he can read the org but is not an
// admin, so the grant-only _acl endpoint is forbidden.
func TestSeedWebUIRegularMemberReadsButIsNotAdmin(t *testing.T) {
	srv := startServer(t, Options{StatePath: "../dev/test-repo", EnforceACL: true})
	base := srv.URL() + "/organizations/acme"

	for _, path := range []string{"/nodes", "/nodes/dev-app-01", "/roles", "/environments"} {
		if got := statusOf(t, webuiSignedAs(t, srv, "jack", "GET", base+path, "")); got != http.StatusOK {
			t.Errorf("jack GET %s = %d, want 200", path, got)
		}
	}

	if got := statusOf(t, webuiSignedAs(t, srv, "jack", "GET", base+"/nodes/dev-app-01/_acl", "")); got != http.StatusForbidden {
		t.Errorf("jack GET node _acl = %d, want 403 (jack is not an admin)", got)
	}
}

// A global user who is not associated with acme inherits no group grant and is
// denied — membership in the org's "users" group is what unlocks read access.
func TestSeedWebUIUnassociatedUserDenied(t *testing.T) {
	srv := startServer(t, Options{StatePath: "../dev/test-repo", EnforceACL: true})
	base := srv.URL() + "/organizations/acme"

	createUser(t, srv, `{"name":"mallory"}`)
	if got := statusOf(t, webuiSignedAs(t, srv, "mallory", "GET", base+"/nodes", "")); got != http.StatusForbidden {
		t.Errorf("unassociated mallory GET nodes = %d, want 403", got)
	}
}
