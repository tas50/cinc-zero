package store

import (
	"sort"
	"sync"
)

// memoryBackend is the in-memory Backend: the default, ephemeral backend that
// keeps all state in maps. It lives in the store package (rather than a subpackage)
// so New can default to it without an import cycle, and it is the reference
// implementation the shared conformance suite is written against.
type memoryBackend struct {
	mu    sync.RWMutex
	orgs  map[string]bool                         // named orgs (for ListOrgs/HasOrg)
	data  map[string]map[string]map[string][]byte // org -> coll -> key -> body
	blobs map[string]map[string][]byte            // org -> checksum -> content
}

// NewMemoryBackend returns an empty in-memory Backend.
func NewMemoryBackend() Backend {
	return &memoryBackend{
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

func (m *memoryBackend) Get(org, coll, key string) ([]byte, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[org][coll][key]
	if !ok {
		return nil, false, nil
	}
	return clone(v), true, nil
}

// set stores a defensive copy of val. Caller holds the write lock.
func (m *memoryBackend) set(org, coll, key string, val []byte) {
	if m.data[org] == nil {
		m.data[org] = map[string]map[string][]byte{}
	}
	if m.data[org][coll] == nil {
		m.data[org][coll] = map[string][]byte{}
	}
	m.data[org][coll][key] = clone(val)
}

func (m *memoryBackend) Put(org, coll, key string, val []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.set(org, coll, key, val)
	return nil
}

func (m *memoryBackend) Create(org, coll, key string, val []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[org][coll][key]; ok {
		return ErrConflict
	}
	m.set(org, coll, key, val)
	return nil
}

func (m *memoryBackend) Delete(org, coll, key string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[org][coll][key]
	if !ok {
		return nil, false, nil
	}
	delete(m.data[org][coll], key)
	return clone(v), true, nil
}

func (m *memoryBackend) Keys(org, coll string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]string, 0, len(m.data[org][coll]))
	for k := range m.data[org][coll] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

func (m *memoryBackend) Range(org, coll string, fn func(key string, raw []byte) bool) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for k, v := range m.data[org][coll] {
		if !fn(k, v) {
			return nil
		}
	}
	return nil
}

func (m *memoryBackend) Collections(org string) ([]string, error) {
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

func (m *memoryBackend) PutBlob(org, checksum string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.blobs[org] == nil {
		m.blobs[org] = map[string][]byte{}
	}
	m.blobs[org][checksum] = clone(data)
	return nil
}

func (m *memoryBackend) Blob(org, checksum string) ([]byte, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.blobs[org][checksum]
	if !ok {
		return nil, false, nil
	}
	return clone(v), true, nil
}

func (m *memoryBackend) HasBlob(org, checksum string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.blobs[org][checksum]
	return ok, nil
}

func (m *memoryBackend) DeleteBlob(org, checksum string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.blobs[org], checksum)
	return nil
}

func (m *memoryBackend) CreateOrg(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.orgs[name] {
		return ErrConflict
	}
	m.orgs[name] = true
	return nil
}

func (m *memoryBackend) DeleteOrg(name string) (bool, error) {
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

func (m *memoryBackend) ListOrgs() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.orgs))
	for n := range m.orgs {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

func (m *memoryBackend) HasOrg(name string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.orgs[name], nil
}

func (m *memoryBackend) Close() error { return nil }
