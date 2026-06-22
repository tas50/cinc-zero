package server

import (
	"net/http"
	"strings"
	"testing"
)

// TestMaxBodyBytesRejectsOversizedRequest verifies the configured body cap is
// enforced: a request under the limit succeeds, one over it is rejected with a
// 4xx (rather than being read unbounded into memory).
func TestMaxBodyBytesRejectsOversizedRequest(t *testing.T) {
	srv := startServer(t, Options{DisableAuth: true, MaxBodyBytes: 512})
	url := srv.URL() + "/organizations/acme/nodes"

	resp, err := http.Post(url, "application/json", strings.NewReader(`{"name":"small"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("small body = %d, want 201", resp.StatusCode)
	}

	big := `{"name":"big","blob":"` + strings.Repeat("a", 2048) + `"}`
	resp, err = http.Post(url, "application/json", strings.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Fatalf("oversized body = %d, want a 4xx rejection", resp.StatusCode)
	}
}

// TestMaxBodyBytesNegativeDisablesLimit verifies a negative cap turns the limit
// off: a body that would exceed the default is accepted.
func TestMaxBodyBytesNegativeDisablesLimit(t *testing.T) {
	srv := startServer(t, Options{DisableAuth: true, MaxBodyBytes: -1})
	url := srv.URL() + "/organizations/acme/nodes"

	big := `{"name":"big","blob":"` + strings.Repeat("a", 8192) + `"}`
	resp, err := http.Post(url, "application/json", strings.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("large body with limit disabled = %d, want 201", resp.StatusCode)
	}
}

// TestMaxBodyBytesDefault verifies the zero value gets a sane (non-zero,
// positive) default rather than an unlimited body.
func TestMaxBodyBytesDefault(t *testing.T) {
	o := Options{}
	o.withDefaults()
	if o.MaxBodyBytes <= 0 {
		t.Fatalf("default MaxBodyBytes = %d, want a positive cap", o.MaxBodyBytes)
	}
}
