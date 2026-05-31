package auth

import (
	"bufio"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"testing"
)

// TestGenerateKeyFromProducesValidKey verifies GenerateKeyFrom reads entropy
// from the supplied reader and yields a usable 2048-bit key — the seam that lets
// the bootstrap give each parallel keygen its own buffered entropy source to
// avoid contending on the global crypto/rand reader.
func TestGenerateKeyFromProducesValidKey(t *testing.T) {
	key, err := GenerateKeyFrom(bufio.NewReader(rand.Reader))
	if err != nil {
		t.Fatal(err)
	}
	if key.N.BitLen() != 2048 {
		t.Fatalf("key size = %d bits, want 2048", key.N.BitLen())
	}
	if err := key.Validate(); err != nil {
		t.Fatalf("generated key invalid: %v", err)
	}
}

func TestPrivateKeyPEMRoundTrip(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := EncodePrivateKeyPEM(key)
	got, err := ParsePrivateKey(pemBytes)
	if err != nil {
		t.Fatalf("ParsePrivateKey: %v", err)
	}
	if got.N.Cmp(key.N) != 0 {
		t.Fatal("round-tripped private key differs")
	}
}

func TestPublicKeyPEMRoundTrip(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	pemBytes, err := EncodePublicKeyPEM(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParsePublicKey(pemBytes)
	if err != nil {
		t.Fatalf("ParsePublicKey: %v", err)
	}
	if got.N.Cmp(key.N) != 0 || got.E != key.E {
		t.Fatal("round-tripped public key differs")
	}
}

func TestParsePublicKeyAcceptsPKCS1(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	pkcs1 := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey),
	})
	if _, err := ParsePublicKey(pkcs1); err != nil {
		t.Fatalf("ParsePublicKey PKCS1: %v", err)
	}
}

func TestParsePrivateKeyAcceptsPKCS8(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8 := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if _, err := ParsePrivateKey(pkcs8); err != nil {
		t.Fatalf("ParsePrivateKey PKCS8: %v", err)
	}
}

func TestParseKeyErrors(t *testing.T) {
	if _, err := ParsePublicKey([]byte("not a pem")); err == nil {
		t.Fatal("expected error for non-PEM public key")
	}
	if _, err := ParsePrivateKey([]byte("not a pem")); err == nil {
		t.Fatal("expected error for non-PEM private key")
	}
	badType := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte{1, 2, 3}})
	if _, err := ParsePublicKey(badType); err == nil {
		t.Fatal("expected error for wrong PEM type")
	}
}
