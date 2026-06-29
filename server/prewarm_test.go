package server

import "testing"

// TestNewDoesNotPersistPrewarmState ensures the startup pre-warm only issues
// read-only or validation-failing requests: a freshly built server must contain
// exactly its bootstrap state and none of the synthetic warm-up objects.
func TestNewDoesNotPersistPrewarmState(t *testing.T) {
	for _, auth := range []bool{false, true} {
		s := startServer(t, Options{Orgs: []string{"acme"}, DisableAuth: !auth})
		org, ok, err := s.Store().Org("acme")
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatal("org acme missing")
		}
		nodes, err := org.Keys("nodes")
		if err != nil {
			t.Fatal(err)
		}
		if len(nodes) != 0 {
			t.Fatalf("auth=%v: pre-warm persisted nodes: %v", auth, nodes)
		}
		clients, err := org.Keys("clients")
		if err != nil {
			t.Fatal(err)
		}
		if len(clients) != 1 || clients[0] != "acme-validator" {
			t.Fatalf("auth=%v: unexpected clients after pre-warm: %v", auth, clients)
		}
	}
}
