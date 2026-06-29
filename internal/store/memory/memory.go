// Package memory is the in-memory store.Backend: the default, ephemeral backend
// that keeps all state in maps. It is the reference implementation the shared
// conformance suite is written against.
package memory

import (
	"sort"
	"sync"

	"github.com/tas50/cinc-zero/internal/store"
)

// Backend is an in-memory store.Backend. The zero value is not usable; call New.
type Backend struct {
	mu    sync.RWMutex
	orgs  map[string]bool                         // named orgs (for ListOrgs/HasOrg)
	data  map[string]map[string]map[string][]byte // org -> coll -> key -> body
	blobs map[string]map[string][]byte            // org -> checksum -> content
}

// New returns an empty in-memory backend.
func New() *Backend {
	return &Backend{
		orgs:  map[string]bool{},
		data:  map[string]map[string]map[string][]byte{},
		blobs: map[string]map[string][]byte{},
	}
}

func clone(b []byte) []byte {
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp
}

func (m *Backend) Get(org, coll, key string) ([]byte, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[org][coll][key]
	if !ok {
		return nil, false, nil
	}
	return clone(v), true, nil
}

// set stores a defensive copy of val. Caller holds the write lock.
func (m *Backend) set(org, coll, key string, val []byte) {
	if m.data[org] == nil {
		m.data[org] = map[string]map[string][]byte{}
	}
	if m.data[org][coll] == nil {
		m.data[org][coll] = map[string][]byte{}
	}
	m.data[org][coll][key] = clone(val)
}

func (m *Backend) Put(org, coll, key string, val []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.set(org, coll, key, val)
	return nil
}

func (m *Backend) Create(org, coll, key string, val []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[org][coll][key]; ok {
		return store.ErrConflict
	}
	m.set(org, coll, key, val)
	return nil
}

func (m *Backend) Delete(org, coll, key string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[org][coll][key]
	if !ok {
		return nil, false, nil
	}
	delete(m.data[org][coll], key)
	return clone(v), true, nil
}

func (m *Backend) Keys(org, coll string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]string, 0, len(m.data[org][coll]))
	for k := range m.data[org][coll] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

func (m *Backend) Range(org, coll string, fn func(key string, raw []byte) bool) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for k, v := range m.data[org][coll] {
		if !fn(k, v) {
			return nil
		}
	}
	return nil
}

func (m *Backend) Collections(org string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	colls := make([]string, 0, len(m.data[org]))
	for c, keys := range m.data[org] {
		if len(keys) > 0 {
			colls = append(colls, c)
		}
	}
	sort.Strings(colls)
	return colls, nil
}

func (m *Backend) PutBlob(org, checksum string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.blobs[org] == nil {
		m.blobs[org] = map[string][]byte{}
	}
	m.blobs[org][checksum] = clone(data)
	return nil
}

func (m *Backend) Blob(org, checksum string) ([]byte, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.blobs[org][checksum]
	if !ok {
		return nil, false, nil
	}
	return clone(v), true, nil
}

func (m *Backend) HasBlob(org, checksum string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.blobs[org][checksum]
	return ok, nil
}

func (m *Backend) DeleteBlob(org, checksum string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.blobs[org], checksum)
	return nil
}

func (m *Backend) CreateOrg(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.orgs[name] {
		return store.ErrConflict
	}
	m.orgs[name] = true
	return nil
}

func (m *Backend) DeleteOrg(name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.orgs[name] {
		return false, nil
	}
	delete(m.orgs, name)
	delete(m.data, name)
	delete(m.blobs, name)
	return true, nil
}

func (m *Backend) ListOrgs() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.orgs))
	for n := range m.orgs {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

func (m *Backend) HasOrg(name string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.orgs[name], nil
}

func (m *Backend) Close() error { return nil }
