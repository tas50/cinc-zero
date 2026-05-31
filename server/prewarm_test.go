package server

import "testing"

// TestNewDoesNotPersistPrewarmState ensures the startup pre-warm only issues
// read-only or validation-failing requests: a freshly built server must contain
// exactly its bootstrap state and none of the synthetic warm-up objects.
func TestNewDoesNotPersistPrewarmState(t *testing.T) {
	for _, auth := range []bool{false, true} {
		s := startServer(t, Options{Orgs: []string{"acme"}, DisableAuth: !auth})
		org, ok := s.Store().Org("acme")
		if !ok {
			t.Fatal("org acme missing")
		}
		if nodes := org.Keys("nodes"); len(nodes) != 0 {
			t.Fatalf("auth=%v: pre-warm persisted nodes: %v", auth, nodes)
		}
		clients := org.Keys("clients")
		if len(clients) != 1 || clients[0] != "acme-validator" {
			t.Fatalf("auth=%v: unexpected clients after pre-warm: %v", auth, clients)
		}
	}
}
