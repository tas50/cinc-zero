// Package store is the in-memory data store for cinc-zero. All Chef objects
// live here, namespaced by organization then by collection (e.g. "nodes",
// "roles") then by key. Values are stored as raw canonical JSON so client
// payloads round-trip exactly.
package store

import (
	"errors"
	"sort"
	"sync"
)

var (
	// ErrConflict is returned by Create when the key already exists.
	ErrConflict = errors.New("store: already exists")
	// ErrNotFound is returned when a requested object does not exist.
	ErrNotFound = errors.New("store: not found")
)

// Store holds every organization's data plus a global space for server-level
// objects (users and organization metadata) that live outside any org.
type Store struct {
	mu     sync.RWMutex
	orgs   map[string]*Org
	global *Org
}

// New returns an empty Store.
func New() *Store {
	return &Store{
		orgs:   make(map[string]*Org),
		global: &Org{name: "", data: make(map[string]map[string][]byte)},
	}
}

// Global returns the server-global object space, used for collections such as
// "users" and "organizations" that are not scoped to an organization.
func (s *Store) Global() *Org {
	return s.global
}

// CreateOrg creates a new, empty organization. It returns ErrConflict if an
// organization with the same name already exists.
func (s *Store) CreateOrg(name string) (*Org, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[name]; ok {
		return nil, ErrConflict
	}
	org := &Org{name: name, data: make(map[string]map[string][]byte)}
	s.orgs[name] = org
	return org, nil
}

// Org returns the named organization.
func (s *Store) Org(name string) (*Org, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	org, ok := s.orgs[name]
	return org, ok
}

// DeleteOrg removes an organization, reporting whether it existed.
func (s *Store) DeleteOrg(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[name]; !ok {
		return false
	}
	delete(s.orgs, name)
	return true
}

// ListOrgs returns the organization names in sorted order.
func (s *Store) ListOrgs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.orgs))
	for name := range s.orgs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Org is a single organization's collection of objects.
type Org struct {
	mu    sync.RWMutex
	name  string
	data  map[string]map[string][]byte // collection -> key -> raw JSON
	blobs map[string][]byte            // checksum -> raw file content
}

// Name returns the organization name.
func (o *Org) Name() string { return o.name }

// Create stores val under coll/key, returning ErrConflict if it already exists.
func (o *Org) Create(coll, key string, val []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, ok := o.data[coll][key]; ok {
		return ErrConflict
	}
	o.set(coll, key, val)
	return nil
}

// Put stores val under coll/key, overwriting any existing value.
func (o *Org) Put(coll, key string, val []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.set(coll, key, val)
}

// set stores a defensive copy of val. Caller must hold the write lock.
func (o *Org) set(coll, key string, val []byte) {
	if o.data[coll] == nil {
		o.data[coll] = make(map[string][]byte)
	}
	cp := make([]byte, len(val))
	copy(cp, val)
	o.data[coll][key] = cp
}

// Get returns a copy of the value at coll/key.
func (o *Org) Get(coll, key string) ([]byte, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	val, ok := o.data[coll][key]
	if !ok {
		return nil, false
	}
	cp := make([]byte, len(val))
	copy(cp, val)
	return cp, true
}

// Delete removes coll/key, returning the removed value and whether it existed.
func (o *Org) Delete(coll, key string) ([]byte, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	val, ok := o.data[coll][key]
	if !ok {
		return nil, false
	}
	delete(o.data[coll], key)
	return val, true
}

// Keys returns the sorted keys in a collection.
func (o *Org) Keys(coll string) []string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	keys := make([]string, 0, len(o.data[coll]))
	for k := range o.data[coll] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// PutBlob stores raw file content keyed by its checksum (hex MD5). The Chef
// cookbook upload flow uploads file bodies here before a cookbook manifest
// referencing those checksums is created.
func (o *Org) PutBlob(checksum string, data []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.blobs == nil {
		o.blobs = make(map[string][]byte)
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	o.blobs[checksum] = cp
}

// Blob returns a copy of the blob stored under checksum and whether it exists.
func (o *Org) Blob(checksum string) ([]byte, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	val, ok := o.blobs[checksum]
	if !ok {
		return nil, false
	}
	cp := make([]byte, len(val))
	copy(cp, val)
	return cp, true
}

// HasBlob reports whether a blob with the given checksum has been uploaded.
func (o *Org) HasBlob(checksum string) bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	_, ok := o.blobs[checksum]
	return ok
}

// DeleteBlob removes the blob stored under checksum, if any. It is used to
// garbage-collect file-store content no longer referenced by any cookbook or
// cookbook_artifact manifest.
func (o *Org) DeleteBlob(checksum string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.blobs, checksum)
}

// Collections returns the sorted collection names that contain at least one key.
func (o *Org) Collections() []string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	colls := make([]string, 0, len(o.data))
	for c, m := range o.data {
		if len(m) > 0 {
			colls = append(colls, c)
		}
	}
	sort.Strings(colls)
	return colls
}
