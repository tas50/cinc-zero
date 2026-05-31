package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// errBody decodes a Chef-style {"error":[...]} body, failing if it is absent.
func errBody(t *testing.T, resp *http.Response, body string) {
	t.Helper()
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json; body %s", ct, body)
	}
	var e struct {
		Error []string `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		t.Fatalf("body is not JSON (%v): %s", err, body)
	}
	if len(e.Error) == 0 {
		t.Fatalf("expected a non-empty error array, got %s", body)
	}
}

func TestUnknownRouteReturnsJSON404(t *testing.T) {
	srv, _ := newTestAPI(t)
	resp, body := do(t, "GET", srv.URL+"/this/route/does/not/exist", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown route status = %d, want 404", resp.StatusCode)
	}
	errBody(t, resp, body)
}

func TestUnsupportedMethodReturnsJSON405(t *testing.T) {
	srv, _ := newTestAPI(t)
	// The nodes collection registers GET and POST; DELETE is method-not-allowed.
	resp, body := do(t, "DELETE", srv.URL+"/organizations/acme/nodes", "")
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("unsupported method status = %d, want 405", resp.StatusCode)
	}
	errBody(t, resp, body)
	if allow := resp.Header.Get("Allow"); allow == "" {
		t.Fatalf("405 should advertise allowed methods in Allow header")
	}
}

func TestKnownRouteUnaffected(t *testing.T) {
	srv, _ := newTestAPI(t)
	// A real route still works (the catch-all must not intercept matches).
	resp, body := do(t, "GET", srv.URL+"/organizations/acme/nodes", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("known route status = %d, want 200; body %s", resp.StatusCode, body)
	}
}
