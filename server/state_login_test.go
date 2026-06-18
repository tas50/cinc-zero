package server

import (
	"net/http"
	"testing"
)

// A user seeded from a state directory with a password can be logged in by a
// webui-sourced authenticate_user — the cinc-console login path, end to end.
// The committed dev/test-repo seeds "anna" with the password "anna123".
func TestSeededUserWebUILogin(t *testing.T) {
	srv := startServer(t, Options{StatePath: "../dev/test-repo"})

	if got := statusOf(t, webuiSignedAs(t, srv, "anna", "POST",
		srv.URL()+"/authenticate_user", `{"username":"anna","password":"anna123"}`)); got != http.StatusOK {
		t.Fatalf("seeded anna webui login = %d, want 200", got)
	}

	if got := statusOf(t, webuiSignedAs(t, srv, "anna", "POST",
		srv.URL()+"/authenticate_user", `{"username":"anna","password":"wrong"}`)); got != http.StatusUnauthorized {
		t.Fatalf("seeded anna wrong password = %d, want 401", got)
	}
}
