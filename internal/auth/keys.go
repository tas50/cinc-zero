package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
)

// GenerateKey creates a new 2048-bit RSA key pair, the size Chef uses for
// client and user keys.
func GenerateKey() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, 2048)
}

// ParsePublicKey parses a PEM-encoded RSA public key. It accepts both
// "PUBLIC KEY" (PKIX/SubjectPublicKeyInfo) and "RSA PUBLIC KEY" (PKCS#1)
// blocks, which together cover what Chef stores and what clients send.
func ParsePublicKey(pemBytes []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("auth: no PEM block found in public key")
	}
	switch block.Type {
	case "PUBLIC KEY":
		key, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("auth: parse PKIX public key: %w", err)
		}
		rsaKey, ok := key.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("auth: public key is not RSA")
		}
		return rsaKey, nil
	case "RSA PUBLIC KEY":
		return x509.ParsePKCS1PublicKey(block.Bytes)
	default:
		return nil, fmt.Errorf("auth: unexpected PEM type %q for public key", block.Type)
	}
}

// ParsePrivateKey parses a PEM-encoded RSA private key in PKCS#1 ("RSA PRIVATE
// KEY") or PKCS#8 ("PRIVATE KEY") form.
func ParsePrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("auth: no PEM block found in private key")
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("auth: parse PKCS8 private key: %w", err)
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("auth: private key is not RSA")
		}
		return rsaKey, nil
	default:
		return nil, fmt.Errorf("auth: unexpected PEM type %q for private key", block.Type)
	}
}

// EncodePrivateKeyPEM returns the PKCS#1 PEM encoding of key, the format Chef
// writes for client keys (e.g. the validator and admin keys).
func EncodePrivateKeyPEM(key *rsa.PrivateKey) []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}

// EncodePublicKeyPEM returns the PKIX ("PUBLIC KEY") PEM encoding of pub, the
// format Chef stores and returns for actor public keys.
func EncodePublicKeyPEM(pub *rsa.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}
