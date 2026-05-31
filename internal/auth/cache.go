package auth

import (
	"crypto/rsa"
	"sync"
)

// PublicKeyCache memoizes parsed RSA public keys by their PEM encoding. Parsing
// a PEM-wrapped x509 key is one of the costlier steps on the authentication
// hot path, and a single chef-client / knife run issues many requests signed by
// the same key, so caching the parse turns a repeat lookup into a map read.
//
// Keying on the PEM string makes invalidation automatic: when an actor rotates
// its key the stored PEM changes, so the next request misses the cache and the
// new key is parsed. Parse failures are not cached.
//
// A PublicKeyCache is safe for concurrent use.
type PublicKeyCache struct {
	mu sync.RWMutex
	m  map[string]*rsa.PublicKey
}

// NewPublicKeyCache returns an empty cache ready for use.
func NewPublicKeyCache() *PublicKeyCache {
	return &PublicKeyCache{m: make(map[string]*rsa.PublicKey)}
}

// Parse returns the RSA public key for the given PEM, parsing it (via
// ParsePublicKey) on a miss and caching the result. Errors are returned but not
// cached.
func (c *PublicKeyCache) Parse(pemStr string) (*rsa.PublicKey, error) {
	c.mu.RLock()
	key, ok := c.m[pemStr]
	c.mu.RUnlock()
	if ok {
		return key, nil
	}
	key, err := ParsePublicKey([]byte(pemStr))
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.m[pemStr] = key
	c.mu.Unlock()
	return key, nil
}
