package server

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// TestSQLiteBackedServerRoundTrip exercises the full HTTP -> api -> SQLite store
// path: a node created over HTTP is read back from the durable backend.
func TestSQLiteBackedServerRoundTrip(t *testing.T) {
	db := filepath.Join(t.TempDir(), "srv.db")
	srv := startServer(t, Options{
		Orgs:        []string{"acme"},
		DisableAuth: true,
		Storage:     "sqlite",
		DB:          db,
	})

	base := srv.URL() + "/organizations/acme/nodes"
	resp, err := http.Post(base, "application/json", strings.NewReader(`{"name":"web01","chef_type":"node"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create node: status %d", resp.StatusCode)
	}

	resp, err = http.Get(base + "/web01")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"web01"`) {
		t.Fatalf("get node from sqlite-backed server: status %d body %s", resp.StatusCode, body)
	}
}

// TestSQLiteStorageRequiresDB rejects --storage sqlite with no database path
// rather than silently writing somewhere unexpected.
func TestSQLiteStorageRequiresDB(t *testing.T) {
	if _, err := New(Options{Storage: "sqlite", DisableAuth: true}); err == nil {
		t.Fatal("expected an error when storage is sqlite but no DB path is set")
	}
}

// TestUnknownStorageRejected rejects an unrecognized backend name.
func TestUnknownStorageRejected(t *testing.T) {
	if _, err := New(Options{Storage: "bogus", DisableAuth: true}); err == nil {
		t.Fatal("expected an error for an unknown storage backend")
	}
}
