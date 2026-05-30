package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestStatsEndpoint(t *testing.T) {
	srv, _ := newTestAPI(t)
	resp, body := do(t, "GET", srv.URL+"/_stats", "")
	if resp.StatusCode != 200 {
		t.Fatalf("_stats = %d", resp.StatusCode)
	}
	// Body must be valid JSON.
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("_stats body not JSON: %v (%s)", err, body)
	}
}

func TestRequiredRecipeDisabled(t *testing.T) {
	srv, _ := newTestAPI(t)
	// Disabled by default, like a stock Chef server.
	resp, _ := do(t, "GET", srv.URL+"/organizations/acme/required_recipe", "")
	if resp.StatusCode != 404 {
		t.Fatalf("required_recipe = %d, want 404", resp.StatusCode)
	}
}

func TestPrincipals(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/organizations/acme"

	// A global user.
	do(t, "POST", srv.URL+"/users", `{"name":"alice"}`)
	_, body := do(t, "GET", base+"/principals/alice", "")
	var u struct {
		Name      string `json:"name"`
		Type      string `json:"type"`
		PublicKey string `json:"public_key"`
	}
	json.Unmarshal([]byte(body), &u)
	if u.Name != "alice" || u.Type != "user" || !strings.Contains(u.PublicKey, "PUBLIC KEY") {
		t.Fatalf("user principal = %s", body)
	}

	// An org client.
	do(t, "POST", base+"/clients", `{"name":"web01"}`)
	_, body = do(t, "GET", base+"/principals/web01", "")
	json.Unmarshal([]byte(body), &u)
	if u.Type != "client" {
		t.Fatalf("client principal type = %s", body)
	}

	// Unknown principal.
	resp, _ := do(t, "GET", base+"/principals/ghost", "")
	if resp.StatusCode != 404 {
		t.Fatalf("unknown principal = %d, want 404", resp.StatusCode)
	}
}

func TestAPIVersionHeader(t *testing.T) {
	srv, _ := newTestAPI(t)

	// Every response advertises the supported server API version range.
	resp, _ := do(t, "GET", srv.URL+"/organizations/acme/nodes", "")
	hdr := resp.Header.Get("X-Ops-Server-API-Version")
	if hdr == "" {
		t.Fatalf("missing X-Ops-Server-API-Version header")
	}
	var neg struct {
		MinVersion string `json:"min_version"`
		MaxVersion string `json:"max_version"`
	}
	if err := json.Unmarshal([]byte(hdr), &neg); err != nil {
		t.Fatalf("version header not JSON: %v (%s)", err, hdr)
	}
	if neg.MinVersion == "" || neg.MaxVersion == "" {
		t.Fatalf("version header missing bounds: %s", hdr)
	}

	// Requesting an unsupported version is rejected with 406.
	req, _ := http.NewRequest("GET", srv.URL+"/organizations/acme/nodes", nil)
	req.Header.Set("X-Ops-Server-API-Version", "99")
	got, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	if got.StatusCode != http.StatusNotAcceptable {
		t.Fatalf("unsupported version = %d, want 406", got.StatusCode)
	}
	if got.Header.Get("X-Ops-Server-API-Version") == "" {
		t.Fatalf("406 response missing version header")
	}
}
