package server

import (
	"io"
	"net/http"
	"strings"
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

// The console's org picker lists a logged-in user's orgs; the seeded anna
// belongs to acme.
func TestSeededUserBelongsToOrg(t *testing.T) {
	srv := startServer(t, Options{StatePath: "../dev/test-repo"})

	resp, err := http.DefaultClient.Do(webuiSignedAs(t, srv, "anna", "GET",
		srv.URL()+"/users/anna/organizations", ""))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list anna orgs = %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"acme"`) {
		t.Fatalf("anna's orgs do not include acme: %s", body)
	}
}
