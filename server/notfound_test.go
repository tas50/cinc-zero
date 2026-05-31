package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestSignedUnknownRouteReturnsJSON404 proves the JSON catch-all survives the
// full middleware stack (auth → version negotiation → mux): an authenticated
// request to a path with no route returns a Chef-shaped JSON 404, not Go's
// plaintext default.
func TestSignedUnknownRouteReturnsJSON404(t *testing.T) {
	srv := startServer(t, Options{Orgs: []string{"acme"}})
	resp, err := http.DefaultClient.Do(signed(t, srv, "GET", srv.URL()+"/organizations/acme/nope", ""))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown route status = %d, want 404; body %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var e struct {
		Error []string `json:"error"`
	}
	if json.Unmarshal(body, &e) != nil || len(e.Error) == 0 {
		t.Fatalf("expected JSON error array, got %s", body)
	}
}
