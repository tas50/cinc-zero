package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"testing"
)

// fuzzKey is a single RSA key shared by the verification fuzz targets. It is
// generated once per test process (not per execution), so it adds no per-exec
// cost. The no-panic contract holds for any key, so its exact value is
// immaterial.
var fuzzKey = func() *rsa.PrivateKey {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return k
}()

// header builds an http.Header from the X-Ops-* fields the auth layer reads.
func authHeader(sign, userID, ts, contentHash, apiVersion, auth1, auth2 string) http.Header {
	h := http.Header{}
	h.Set("X-Ops-Sign", sign)
	h.Set("X-Ops-Userid", userID)
	h.Set("X-Ops-Timestamp", ts)
	h.Set("X-Ops-Content-Hash", contentHash)
	h.Set("X-Ops-Server-API-Version", apiVersion)
	h.Set("X-Ops-Authorization-1", auth1)
	h.Set("X-Ops-Authorization-2", auth2)
	return h
}

// FuzzParse exercises the X-Ops-* header parser with arbitrary header values.
// Contract: Parse must never panic, and a nil error must yield a non-nil result.
func FuzzParse(f *testing.F) {
	f.Add("algorithm=sha1;version=1.0;", "alice", "2026-01-01T00:00:00Z", "abc=", "1", "AAAA", "")
	f.Add("algorithm=sha256;version=1.3;", "bob", "", "", "", "", "")
	f.Add("version=1.1", "", "", "", "0", "====", "not-base64!!")
	f.Add("", "", "", "", "", "", "")
	f.Add(";;;;", "u", "t", "c", "x", "%%%%", "????")

	f.Fuzz(func(t *testing.T, sign, userID, ts, contentHash, apiVersion, auth1, auth2 string) {
		p, err := Parse(authHeader(sign, userID, ts, contentHash, apiVersion, auth1, auth2))
		if err == nil && p == nil {
			t.Fatalf("Parse returned a nil result with a nil error (sign=%q)", sign)
		}
	})
}

// FuzzVerifyRequest drives the whole verification path — header parsing,
// canonical-string construction, and signature verification (including the
// hand-rolled public-key decrypt for v1.0/1.1) — against a fixed key with
// attacker-shaped inputs. Verification is expected to fail for essentially all
// inputs; the contract is only that it never panics.
func FuzzVerifyRequest(f *testing.F) {
	f.Add("GET", "/organizations/acme/nodes", []byte(""),
		"algorithm=sha256;version=1.3;", "alice", "2026-01-01T00:00:00Z", "AAAA", "1")
	f.Add("POST", "//a///b/", []byte("{}"),
		"algorithm=sha1;version=1.0;", "bob", "t", "////", "")
	f.Add("PUT", "/", []byte("body"),
		"version=1.1", "carol", "", "deadbeef", "MMMM")

	f.Fuzz(func(t *testing.T, method, path string, body []byte, sign, userID, ts, auth1, apiVersion string) {
		h := authHeader(sign, userID, ts, "", apiVersion, auth1, "")
		// Must not panic regardless of the (almost certainly invalid) signature.
		_ = VerifyRequest(method, path, body, h, &fuzzKey.PublicKey)
	})
}

// FuzzPublicDecrypt targets the hand-written PKCS#1 v1.5 public-key decrypt /
// unpadding directly — manual big.Int math and byte indexing on attacker bytes,
// the highest-risk routine in the package. It must never panic for any input.
func FuzzPublicDecrypt(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x00})
	f.Add([]byte{0x00, 0x01, 0xff, 0x00, 0x42})
	f.Add(make([]byte, 256))
	big := make([]byte, 256)
	for i := range big {
		big[i] = 0xff
	}
	f.Add(big)

	f.Fuzz(func(t *testing.T, sig []byte) {
		_, _ = rsaPublicDecrypt(&fuzzKey.PublicKey, sig)
	})
}
