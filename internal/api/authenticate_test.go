package api

import (
	"strings"
	"testing"
)

func TestAuthenticateUser(t *testing.T) {
	srv, _ := newTestAPI(t)

	// Create a global user with a password.
	resp, body := do(t, "POST", srv.URL+"/users", `{"name":"alice","password":"s3cret"}`)
	if resp.StatusCode != 201 {
		t.Fatalf("create user = %d: %s", resp.StatusCode, body)
	}

	// The password is never echoed back when the user is read.
	_, body = do(t, "GET", srv.URL+"/users/alice", "")
	if strings.Contains(body, "password") || strings.Contains(body, "s3cret") {
		t.Fatalf("user record leaked password: %s", body)
	}

	// Correct password authenticates.
	resp, body = do(t, "POST", srv.URL+"/authenticate_user", `{"name":"alice","password":"s3cret"}`)
	if resp.StatusCode != 200 {
		t.Fatalf("authenticate (correct) = %d: %s", resp.StatusCode, body)
	}

	// Wrong password is rejected.
	resp, _ = do(t, "POST", srv.URL+"/authenticate_user", `{"name":"alice","password":"nope"}`)
	if resp.StatusCode != 401 {
		t.Fatalf("authenticate (wrong) = %d, want 401", resp.StatusCode)
	}

	// Unknown user is rejected.
	resp, _ = do(t, "POST", srv.URL+"/authenticate_user", `{"name":"ghost","password":"x"}`)
	if resp.StatusCode != 401 {
		t.Fatalf("authenticate (unknown) = %d, want 401", resp.StatusCode)
	}
}
