package server

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// A user seeded from a state directory with a password can be logged in by a
// webui-sourced authenticate_user — the cinc-console login path, end to end.
// The committed dev/test-repo seeds "tim" with the password "tim123".
func TestSeededUserWebUILogin(t *testing.T) {
	srv := startServer(t, Options{StatePath: "../dev/test-repo"})

	if got := statusOf(t, webuiSignedAs(t, srv, "tim", "POST",
		srv.URL()+"/authenticate_user", `{"username":"tim","password":"tim123"}`)); got != http.StatusOK {
		t.Fatalf("seeded tim webui login = %d, want 200", got)
	}

	if got := statusOf(t, webuiSignedAs(t, srv, "tim", "POST",
		srv.URL()+"/authenticate_user", `{"username":"tim","password":"wrong"}`)); got != http.StatusUnauthorized {
		t.Fatalf("seeded tim wrong password = %d, want 401", got)
	}
}

// The console's org picker lists a logged-in user's orgs; the seeded tim
// belongs to acme.
func TestSeededUserBelongsToOrg(t *testing.T) {
	srv := startServer(t, Options{StatePath: "../dev/test-repo"})

	resp, err := http.DefaultClient.Do(webuiSignedAs(t, srv, "tim", "GET",
		srv.URL()+"/users/tim/organizations", ""))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list tim orgs = %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"acme"`) {
		t.Fatalf("tim's orgs do not include acme: %s", body)
	}
}
