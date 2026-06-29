package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func decodeStringError(t *testing.T, body string) string {
	t.Helper()
	var e struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		t.Fatalf("error body not a JSON object with a string error: %v (%s)", err, body)
	}
	return e.Error
}

// Duplicate association / invitation are 409 conflicts with string error bodies.
func TestAssociationConflicts(t *testing.T) {
	srv, _ := newTestAPI(t)
	do(t, "POST", srv.URL+"/users", `{"name":"dave"}`)

	resp, body := do(t, "POST", srv.URL+"/organizations/acme/users", `{"username":"dave"}`)
	if resp.StatusCode != 201 {
		t.Fatalf("associate = %d: %s", resp.StatusCode, body)
	}
	resp, body = do(t, "POST", srv.URL+"/organizations/acme/users", `{"username":"dave"}`)
	if resp.StatusCode != 409 {
		t.Fatalf("dup associate = %d, want 409: %s", resp.StatusCode, body)
	}
	if msg := decodeStringError(t, body); msg != "The association already exists." {
		t.Fatalf("dup associate body = %q", msg)
	}

	do(t, "POST", srv.URL+"/users", `{"name":"erin"}`)
	resp, body = do(t, "POST", srv.URL+"/organizations/acme/association_requests", `{"user":"erin"}`)
	if resp.StatusCode != 201 {
		t.Fatalf("invite = %d: %s", resp.StatusCode, body)
	}
	resp, body = do(t, "POST", srv.URL+"/organizations/acme/association_requests", `{"user":"erin"}`)
	if resp.StatusCode != 409 {
		t.Fatalf("dup invite = %d, want 409: %s", resp.StatusCode, body)
	}
	if msg := decodeStringError(t, body); msg != "The invitation already exists." {
		t.Fatalf("dup invite body = %q", msg)
	}
}

// A rescinded / consumed / nonexistent invite returns 404 with a string body.
func TestInviteConsumed(t *testing.T) {
	srv, _ := newTestAPI(t)
	do(t, "POST", srv.URL+"/users", `{"name":"frank"}`)
	do(t, "POST", srv.URL+"/organizations/acme/association_requests", `{"user":"frank"}`)
	id := "frank-acme"

	resp, body := do(t, "PUT", srv.URL+"/users/frank/association_requests/"+id, `{"response":"reject"}`)
	if resp.StatusCode != 200 {
		t.Fatalf("reject = %d: %s", resp.StatusCode, body)
	}
	resp, body = do(t, "PUT", srv.URL+"/users/frank/association_requests/"+id, `{"response":"accept"}`)
	if resp.StatusCode != 404 {
		t.Fatalf("respond consumed = %d, want 404: %s", resp.StatusCode, body)
	}
	if msg := decodeStringError(t, body); !strings.Contains(msg, "Cannot find association request") {
		t.Fatalf("consumed body = %q", msg)
	}
	resp, body = do(t, "DELETE", srv.URL+"/organizations/acme/association_requests/"+id, "")
	if resp.StatusCode != 404 {
		t.Fatalf("rescind consumed = %d, want 404: %s", resp.StatusCode, body)
	}
	if msg := decodeStringError(t, body); !strings.Contains(msg, "Cannot find association request") {
		t.Fatalf("rescind body = %q", msg)
	}
}

// An invite whose recorded inviter has lost the authority to invite cannot be
// accepted.
func TestInviterLostAuthority(t *testing.T) {
	srv, st := newTestAPI(t)
	do(t, "POST", srv.URL+"/users", `{"name":"grace"}`)
	org, ok, err := st.Org("acme")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("acme org missing")
	}
	id := "grace-acme"
	if err := org.Put(assocReqColl, id, []byte(`{"id":"grace-acme","username":"grace","orgname":"acme","inviter":"ghostboss"}`)); err != nil {
		t.Fatal(err)
	}

	resp, body := do(t, "PUT", srv.URL+"/users/grace/association_requests/"+id, `{"response":"accept"}`)
	if resp.StatusCode != 403 {
		t.Fatalf("accept stale invite = %d, want 403: %s", resp.StatusCode, body)
	}
}
