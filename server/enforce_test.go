package server

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/tas50/cinc-zero/internal/auth"
)

// signedAs builds a request signed as an arbitrary actor (name + PEM key),
// letting tests act as someone other than the bootstrap admin.
func signedAs(t *testing.T, name string, keyPEM []byte, method, url, body string) *http.Request {
	t.Helper()
	key, err := auth.ParsePrivateKey(keyPEM)
	if err != nil {
		t.Fatalf("parse key for %s: %v", name, err)
	}
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Ops-Server-API-Version", "1")
	ts := time.Now().UTC().Format(time.RFC3339)
	if err := auth.SignRequest(req, name, ts, []byte(body), key); err != nil {
		t.Fatalf("sign as %s: %v", name, err)
	}
	return req
}

func statusOf(t *testing.T, req *http.Request) int {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestEnforceACLRequiresAuth(t *testing.T) {
	_, err := New(Options{DisableAuth: true, EnforceACL: true})
	if err == nil {
		t.Fatal("New should reject EnforceACL together with DisableAuth")
	}
}

func TestEnforceACLEndToEnd(t *testing.T) {
	srv := startServer(t, Options{Orgs: []string{"acme"}, EnforceACL: true})
	base := srv.URL() + "/organizations/acme"
	const validator = "acme-validator"
	vkey := srv.ValidatorKey("acme")

	// The admin (pivotal superuser) creates a node — bypasses ACLs.
	if code := statusOf(t, signed(t, srv, "POST", base+"/nodes", `{"name":"web01"}`)); code != 201 {
		t.Fatalf("admin create node = %d, want 201", code)
	}

	// The validator is an ordinary client in no group: it cannot read the node.
	if code := statusOf(t, signedAs(t, validator, vkey, "GET", base+"/nodes/web01", "")); code != 403 {
		t.Fatalf("validator read node = %d, want 403", code)
	}

	// A missing object reports 404 even to the unauthorized validator
	// (existence is checked before authorization).
	if code := statusOf(t, signedAs(t, validator, vkey, "GET", base+"/nodes/ghost", "")); code != 404 {
		t.Fatalf("validator read missing node = %d, want 404", code)
	}

	// The validator was seeded with create on the clients container, so it can
	// register a new client.
	if code := statusOf(t, signedAs(t, validator, vkey, "POST", base+"/clients", `{"name":"node1"}`)); code != 201 {
		t.Fatalf("validator create client = %d, want 201", code)
	}

	// The admin can still read the node.
	if code := statusOf(t, signed(t, srv, "GET", base+"/nodes/web01", "")); code != 200 {
		t.Fatalf("admin read node = %d, want 200", code)
	}
}

func TestEnforceACLOffByDefault(t *testing.T) {
	// With enforcement off (default), the validator — in no group — can still
	// read a node, preserving the permissive default.
	srv := startServer(t, Options{Orgs: []string{"acme"}})
	base := srv.URL() + "/organizations/acme"
	if code := statusOf(t, signed(t, srv, "POST", base+"/nodes", `{"name":"web01"}`)); code != 201 {
		t.Fatalf("admin create node = %d, want 201", code)
	}
	vkey := srv.ValidatorKey("acme")
	if code := statusOf(t, signedAs(t, "acme-validator", vkey, "GET", base+"/nodes/web01", "")); code != 200 {
		t.Fatalf("enforcement off: validator read node = %d, want 200", code)
	}
}
