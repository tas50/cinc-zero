// Package store is the data store for cinc-zero. All Chef objects live here,
// namespaced by organization then by collection (e.g. "nodes", "roles") then by
// key. Values are stored as raw canonical JSON so client payloads round-trip
// exactly.
//
// Store and Org are a thin facade over a pluggable Backend (see backend.go): the
// facade owns the canonical-JSON/copy semantics and the org-handle ergonomics,
// while the Backend persists opaque bytes. New defaults to the in-memory backend
// (the ephemeral "zero" experience); a durable backend (e.g. SQLite) is supplied
// via NewWithBackend. Because the Backend can fail (a database read or write may
// error), the data-access methods return an error that callers must handle.
package store

import "errors"

var (
	// ErrConflict is returned by Create when the key already exists.
	ErrConflict = errors.New("store: already exists")
	// ErrNotFound is returned when a requested object does not exist.
	ErrNotFound = errors.New("store: not found")
)

// Store holds every organization's data plus a global space for server-level
// objects (users and organization metadata) that live outside any org. It is a
// facade over a Backend.
type Store struct {
	backend Backend
}

// New returns a Store backed by the default in-memory backend.
func New() *Store {
	return &Store{backend: NewMemoryBackend()}
}

// NewWithBackend returns a Store backed by b. Use this to run against a durable
// backend such as SQLite.
func NewWithBackend(b Backend) *Store {
	return &Store{backend: b}
}

// Backend returns the underlying Backend, for callers (e.g. the server) that need
// to close it or pass it to a transaction.
func (s *Store) Backend() Backend { return s.backend }

// Close releases the underlying backend's resources (e.g. the SQLite handle).
func (s *Store) Close() error { return s.backend.Close() }

// Global returns the server-global object space, used for collections such as
// "users" and "organizations" that are not scoped to an organization.
func (s *Store) Global() *Org {
	return &Org{backend: s.backend, name: ""}
}

// CreateOrg creates a new, empty organization. It returns ErrConflict if an
// organization with the same name already exists.
func (s *Store) CreateOrg(name string) (*Org, error) {
	if err := s.backend.CreateOrg(name); err != nil {
		return nil, err
	}
	return &Org{backend: s.backend, name: name}, nil
}

// Org returns the named organization and whether it exists.
func (s *Store) Org(name string) (*Org, bool, error) {
	ok, err := s.backend.HasOrg(name)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	return &Org{backend: s.backend, name: name}, true, nil
}

// DeleteOrg removes an organization, reporting whether it existed.
func (s *Store) DeleteOrg(name string) (bool, error) {
	return s.backend.DeleteOrg(name)
}

// ListOrgs returns the organization names in sorted order.
func (s *Store) ListOrgs() ([]string, error) {
	return s.backend.ListOrgs()
}

// Tx runs fn as a single atomic transaction over the store: every write fn makes
// through the *Store it receives commits together when fn returns nil, or is
// discarded when fn returns a non-nil error (which Tx propagates). Use it for
// multi-write operations that must not leave partial state behind on failure
// (e.g. creating an organization). The *Store passed to fn must not be used after
// fn returns.
func (s *Store) Tx(fn func(tx *Store) error) error {
	return s.backend.Tx(func(b Backend) error {
		return fn(&Store{backend: b})
	})
}

// Org is a handle to a single organization's collection of objects. The empty
// name addresses the global space (see Store.Global).
type Org struct {
	backend Backend
	name    string
}

// Name returns the organization name.
func (o *Org) Name() string { return o.name }

// Create stores val under coll/key, returning ErrConflict if it already exists.
func (o *Org) Create(coll, key string, val []byte) error {
	return o.backend.Create(o.name, coll, key, val)
}

// Put stores val under coll/key, overwriting any existing value.
func (o *Org) Put(coll, key string, val []byte) error {
	return o.backend.Put(o.name, coll, key, val)
}

// Get returns the value at coll/key. The returned slice is the caller's to keep.
func (o *Org) Get(coll, key string) ([]byte, bool, error) {
	return o.backend.Get(o.name, coll, key)
}

// View returns the value at coll/key. It is retained for callers on read paths
// that previously used a zero-copy read; with the pluggable backend it returns an
// owned copy just like Get (the no-copy optimization is internal to the memory
// backend's Range). Callers must still treat the result as read-only.
func (o *Org) View(coll, key string) ([]byte, bool, error) {
	return o.backend.Get(o.name, coll, key)
}

// Range calls fn for each key/value in coll. The raw slice passed to fn is
// read-only and valid only for the duration of the call: fn must not mutate or
// retain it, and must not call back into a mutating store method. Returning false
// from fn stops iteration early. Callers that need sorted order must sort the
// values they collect.
func (o *Org) Range(coll string, fn func(key string, raw []byte) bool) error {
	return o.backend.Range(o.name, coll, fn)
}

// Delete removes coll/key, returning the removed value and whether it existed.
func (o *Org) Delete(coll, key string) ([]byte, bool, error) {
	return o.backend.Delete(o.name, coll, key)
}

// Keys returns the sorted keys in a collection.
func (o *Org) Keys(coll string) ([]string, error) {
	return o.backend.Keys(o.name, coll)
}

// PutBlob stores raw file content keyed by its checksum (hex MD5). The Chef
// cookbook upload flow uploads file bodies here before a cookbook manifest
// referencing those checksums is created.
func (o *Org) PutBlob(checksum string, data []byte) error {
	return o.backend.PutBlob(o.name, checksum, data)
}

// Blob returns the blob stored under checksum and whether it exists.
func (o *Org) Blob(checksum string) ([]byte, bool, error) {
	return o.backend.Blob(o.name, checksum)
}

// BlobView returns the blob stored under checksum. Like View it now returns an
// owned copy; callers must treat it as read-only.
func (o *Org) BlobView(checksum string) ([]byte, bool, error) {
	return o.backend.Blob(o.name, checksum)
}

// HasBlob reports whether a blob with the given checksum has been uploaded.
func (o *Org) HasBlob(checksum string) (bool, error) {
	return o.backend.HasBlob(o.name, checksum)
}

// DeleteBlob removes the blob stored under checksum, if any. It is used to
// garbage-collect file-store content no longer referenced by any cookbook or
// cookbook_artifact manifest.
func (o *Org) DeleteBlob(checksum string) error {
	return o.backend.DeleteBlob(o.name, checksum)
}

// Collections returns the sorted collection names that contain at least one key.
func (o *Org) Collections() ([]string, error) {
	return o.backend.Collections(o.name)
}
