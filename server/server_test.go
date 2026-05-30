package server

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tas50/cinc-zero/internal/auth"
)

func startServer(t *testing.T, opts Options) *Server {
	t.Helper()
	srv, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		srv.Stop(ctx)
	})
	return srv
}

// signed builds a request signed with the server's admin key.
func signed(t *testing.T, srv *Server, method, url, body string) *http.Request {
	t.Helper()
	key, err := auth.ParsePrivateKey(srv.AdminKey())
	if err != nil {
		t.Fatalf("parse admin key: %v", err)
	}
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Ops-Server-API-Version", "1")
	ts := time.Now().UTC().Format(time.RFC3339)
	if err := auth.SignRequest(req, srv.AdminName(), ts, []byte(body), key); err != nil {
		t.Fatalf("sign: %v", err)
	}
	return req
}

func TestServerStatusUnauthenticated(t *testing.T) {
	srv := startServer(t, Options{})
	resp, err := http.Get(srv.URL() + "/_status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("_status without auth = %d", resp.StatusCode)
	}
}

func TestServerRejectsUnsignedRequest(t *testing.T) {
	srv := startServer(t, Options{})
	resp, err := http.Get(srv.URL() + "/organizations/acme/nodes")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("unsigned request = %d, want 401", resp.StatusCode)
	}
}

func TestServerAcceptsSignedRequestEndToEnd(t *testing.T) {
	srv := startServer(t, Options{Orgs: []string{"acme"}})
	base := srv.URL() + "/organizations/acme"

	// Create a node with a signed request.
	req := signed(t, srv, "POST", base+"/nodes", `{"name":"web01"}`)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("signed create = %d", resp.StatusCode)
	}

	// Read it back, signed.
	req = signed(t, srv, "GET", base+"/nodes/web01", "")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("signed get = %d: %s", resp.StatusCode, b)
	}
}

func TestServerRejectsStaleTimestamp(t *testing.T) {
	srv := startServer(t, Options{Orgs: []string{"acme"}})
	key, _ := auth.ParsePrivateKey(srv.AdminKey())
	req, _ := http.NewRequest("GET", srv.URL()+"/organizations/acme/nodes", nil)
	req.Header.Set("X-Ops-Server-API-Version", "1")
	// Timestamp far outside the allowed skew.
	stale := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	auth.SignRequest(req, srv.AdminName(), stale, nil, key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("stale timestamp = %d, want 401", resp.StatusCode)
	}
}

func TestDisableAuthAllowsUnsigned(t *testing.T) {
	srv := startServer(t, Options{Orgs: []string{"acme"}, DisableAuth: true})
	resp, err := http.Get(srv.URL() + "/organizations/acme/nodes")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("DisableAuth unsigned = %d, want 200", resp.StatusCode)
	}
}

func TestServerLoadsRepo(t *testing.T) {
	dir := t.TempDir()
	nodes := filepath.Join(dir, "nodes")
	if err := os.MkdirAll(nodes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nodes, "web01.json"),
		[]byte(`{"name":"web01","chef_environment":"prod"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := startServer(t, Options{Orgs: []string{"acme"}, DisableAuth: true, Repo: dir})

	resp, err := http.Get(srv.URL() + "/organizations/acme/nodes/web01")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("loaded node GET = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "prod") {
		t.Fatalf("loaded node body = %s", body)
	}
}
