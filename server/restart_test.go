package server

import (
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func stopServer(t *testing.T, s *Server) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// TestSQLiteServerRestartIsIdempotent verifies a SQLite-backed server can be
// stopped and re-created on the same database: New must not crash on the already
// existing org, the bootstrap admin key must stay stable (so existing clients keep
// working), and previously written data must persist.
func TestSQLiteServerRestartIsIdempotent(t *testing.T) {
	db := filepath.Join(t.TempDir(), "restart.db")
	opts := Options{Orgs: []string{"acme"}, Storage: "sqlite", DB: db}

	srv1, err := New(opts)
	if err != nil {
		t.Fatalf("first New: %v", err)
	}
	if err := srv1.Start(); err != nil {
		t.Fatal(err)
	}
	adminKey1 := append([]byte(nil), srv1.AdminKey()...)
	validatorKey1 := append([]byte(nil), srv1.ValidatorKey("acme")...)
	if s := respStatus(t, signed(t, srv1, "POST", srv1.URL()+"/organizations/acme/nodes", `{"name":"web01"}`)); s != http.StatusCreated {
		t.Fatalf("create node on first run: status %d", s)
	}
	stopServer(t, srv1)

	// Restart on the same database.
	srv2, err := New(opts)
	if err != nil {
		t.Fatalf("restart New on existing DB: %v", err)
	}
	if err := srv2.Start(); err != nil {
		t.Fatal(err)
	}
	defer stopServer(t, srv2)

	if !bytes.Equal(srv2.AdminKey(), adminKey1) {
		t.Fatal("admin key changed across restart; existing clients would break")
	}
	if !bytes.Equal(srv2.ValidatorKey("acme"), validatorKey1) {
		t.Fatal("validator key changed across restart")
	}
	// The original admin key still authenticates against the restarted server, and
	// the node written before the restart is still there.
	if s := respStatus(t, signed(t, srv2, "GET", srv2.URL()+"/organizations/acme/nodes/web01", "")); s != http.StatusOK {
		t.Fatalf("node did not persist across restart (or key not stable): status %d", s)
	}
}

// TestMemoryServerIsFreshEachTime confirms the default in-memory backend keeps its
// ephemeral behaviour: a new server starts empty and generates a fresh admin key.
func TestMemoryServerIsFreshEachTime(t *testing.T) {
	srv1 := startServer(t, Options{Orgs: []string{"acme"}})
	srv2 := startServer(t, Options{Orgs: []string{"acme"}})
	if bytes.Equal(srv1.AdminKey(), srv2.AdminKey()) {
		t.Fatal("two independent in-memory servers should not share an admin key")
	}
}
