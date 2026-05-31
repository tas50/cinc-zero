package server

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// TestServerAPIVersionUnauthenticated proves GET /server_api_version is served
// without a signature (like /_status) and reports the supported range.
func TestServerAPIVersionUnauthenticated(t *testing.T) {
	srv := startServer(t, Options{Orgs: []string{"acme"}})
	resp, err := http.Get(srv.URL() + "/server_api_version")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/server_api_version unauthenticated = %d, want 200; body %s", resp.StatusCode, body)
	}
	var v struct {
		MinVersion int `json:"min_version"`
		MaxVersion int `json:"max_version"`
	}
	if json.Unmarshal(body, &v) != nil {
		t.Fatalf("body not JSON: %s", body)
	}
	if v.MaxVersion < v.MinVersion {
		t.Fatalf("bad version range %d..%d", v.MinVersion, v.MaxVersion)
	}
}
