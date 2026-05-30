package auth

import (
	"net/http"
	"strings"
	"testing"
)

func TestSignThenVerifyRoundTrip(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"name":"web01"}`)
	req, _ := http.NewRequest("POST", "http://localhost/organizations/acme/nodes", strings.NewReader(string(body)))
	req.Header.Set("X-Ops-Server-API-Version", "1")

	if err := SignRequest(req, "node1", "2024-01-02T03:04:05Z", body, key); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}
	if err := VerifyRequest(req.Method, req.URL.Path, body, req.Header, &key.PublicKey); err != nil {
		t.Fatalf("VerifyRequest rejected a freshly signed request: %v", err)
	}
}

func TestSignedRequestRejectedByWrongKey(t *testing.T) {
	key, _ := GenerateKey()
	other, _ := GenerateKey()
	body := []byte("")
	req, _ := http.NewRequest("GET", "http://localhost/organizations/acme/nodes", nil)
	if err := SignRequest(req, "node1", "2024-01-02T03:04:05Z", body, key); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRequest(req.Method, req.URL.Path, body, req.Header, &other.PublicKey); err == nil {
		t.Fatal("expected verification to fail with wrong key")
	}
}
