package auth

import "testing"

// BenchmarkParseUncached measures the per-request cost the auth path paid
// before caching: a full PEM+x509 parse on every signature verification.
func BenchmarkParseUncached(b *testing.B) {
	key, _ := GenerateKey()
	pemBytes, _ := EncodePublicKeyPEM(&key.PublicKey)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParsePublicKey(pemBytes); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkParseCached measures the same lookup served from the cache, i.e. the
// cost on every request after the first from a given actor key.
func BenchmarkParseCached(b *testing.B) {
	key, _ := GenerateKey()
	pemBytes, _ := EncodePublicKeyPEM(&key.PublicKey)
	pemStr := string(pemBytes)
	c := NewPublicKeyCache()
	if _, err := c.Parse(pemStr); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := c.Parse(pemStr); err != nil {
			b.Fatal(err)
		}
	}
}

func TestPublicKeyCacheReusesParsedKey(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	pemBytes, err := EncodePublicKeyPEM(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	c := NewPublicKeyCache()
	k1, err := c.Parse(string(pemBytes))
	if err != nil {
		t.Fatalf("first Parse: %v", err)
	}
	k2, err := c.Parse(string(pemBytes))
	if err != nil {
		t.Fatalf("second Parse: %v", err)
	}
	if k1 != k2 {
		t.Fatal("expected the cache to return the same parsed key on a hit")
	}
	if !k1.Equal(&key.PublicKey) {
		t.Fatal("cached key does not match the original public key")
	}
}

func TestPublicKeyCacheReparsesDifferentPEM(t *testing.T) {
	a, _ := GenerateKey()
	b, _ := GenerateKey()
	pa, _ := EncodePublicKeyPEM(&a.PublicKey)
	pb, _ := EncodePublicKeyPEM(&b.PublicKey)
	c := NewPublicKeyCache()
	ka, err := c.Parse(string(pa))
	if err != nil {
		t.Fatal(err)
	}
	kb, err := c.Parse(string(pb))
	if err != nil {
		t.Fatal(err)
	}
	if ka == kb {
		t.Fatal("distinct PEMs should not share a cached key")
	}
	if !ka.Equal(&a.PublicKey) || !kb.Equal(&b.PublicKey) {
		t.Fatal("cache returned the wrong key for a PEM")
	}
}

func TestPublicKeyCacheRejectsInvalidPEMWithoutCaching(t *testing.T) {
	c := NewPublicKeyCache()
	if _, err := c.Parse("not a pem"); err == nil {
		t.Fatal("expected an error for invalid PEM")
	}
	// A subsequent valid Parse must still succeed (the failure was not cached).
	key, _ := GenerateKey()
	p, _ := EncodePublicKeyPEM(&key.PublicKey)
	if _, err := c.Parse(string(p)); err != nil {
		t.Fatalf("valid Parse after invalid: %v", err)
	}
}
