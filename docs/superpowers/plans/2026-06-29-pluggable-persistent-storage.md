# Pluggable, Persistent Storage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make cinc-zero's storage pluggable — a `Backend` interface with `memory` (default) and `sqlite` implementations — so the single-process server can run ephemerally or persist all state durably, with Postgres/RDS reachable later by a driver swap.

**Architecture:** A thin `store.Backend` KV interface persists opaque `(org, collection, key) → bytes` and `(org, checksum) → blob`. `Store`/`Org` become a facade over a `Backend` that owns canonical-JSON/copy semantics and propagates backend errors to callers. `memory` keeps today's maps; `sqlite` uses three opaque-body tables via the pure-Go `modernc.org/sqlite` driver.

**Tech Stack:** Go 1.26, `modernc.org/sqlite` (pure Go, no cgo), `database/sql`, stdlib `testing`.

## Global Constraints

- Module path: `github.com/tas50/cinc-zero`.
- Strict TDD: write the failing test first; `make test && make vet` before every commit.
- `make test` = `go test ./... -race -cover`; the suite must stay green at every commit.
- Memory backend is the default; no flags = today's ephemeral behavior, byte-for-byte.
- SQLite driver MUST be `modernc.org/sqlite` (pure Go) — keep `CGO_ENABLED=0` static builds working.
- Errors are JSON in the API layer (`writeError`); never `http.Error`.
- Stored values are canonical JSON; backends treat bodies as opaque bytes.
- `org == ""` addresses the global space (users/organizations collections).

---

## Phasing into PRs

- **PR 1 (this plan, Tasks 1–6):** the standalone pluggable backend layer — `store.Backend`
  interface, `memory` and `sqlite` backends, and a shared conformance suite proving them
  identical. No existing behavior changes; the layer is not yet wired into `Store`/`Org`.
  Self-contained and fully testable.
- **PR 2 (Tasks 7–10):** wire `Store`/`Org` onto a `Backend`, propagate `error` through the
  ~174 call sites, add `Tx`, add `--storage`/`--db` flags + `server.Options`, atomic org
  create/delete. This is the large mechanical diff; it lands separately because a 174-site
  signature change is unreviewable bundled with the layer above.
- **PR 3 (Tasks 11–12):** docs (container usage, backup recipes, upgrade/migration notes),
  README status-table update, optional `Searcher` capability seam.

This plan details PR 1 to step granularity. PR 2/3 are captured at task granularity; they
get their own step-level expansion (and their own spec-review) when PR 1 merges.

---

## File Structure (PR 1)

- Create `internal/store/backend.go` — the `Backend` interface (errors `ErrConflict`/`ErrNotFound` already live in `store.go`).
- Create `internal/store/memory/memory.go` — `memory.Backend`, maps + `sync.RWMutex`.
- Create `internal/store/memory/memory_test.go` — runs the conformance suite.
- Create `internal/store/backendtest/backendtest.go` — exported `Run(t, newBackend)` conformance suite.
- Create `internal/store/sqlite/sqlite.go` — `sqlite.Backend` over `modernc.org/sqlite`.
- Create `internal/store/sqlite/sqlite_test.go` — runs the conformance suite + sqlite-specific tests.
- Modify `go.mod`/`go.sum` — add `modernc.org/sqlite`.

---

### Task 1: Define the `Backend` interface

**Files:**
- Create: `internal/store/backend.go`

**Interfaces:**
- Produces: `store.Backend` (consumed by Tasks 2–6 and PR 2).

`org == ""` is the global space. Object reads/writes work on any `(org, coll, key)`
regardless of whether `CreateOrg` was called; `CreateOrg`/`ListOrgs`/`HasOrg` track the
*named* org set (the global space is never listed). `DeleteOrg` drops all of that org's
objects and blobs. `Create`/`CreateOrg` return `ErrConflict` on an existing key/org.

- [ ] **Step 1: Write the interface**

```go
// Package store: backend.go
package store

// Backend is the pluggable persistence layer beneath Store/Org. It stores opaque
// canonical-JSON object bodies keyed by (org, collection, key) and opaque blob
// content keyed by (org, checksum). org == "" addresses the global space. A Backend
// must be safe for concurrent use. Implementations live in subpackages (memory, sqlite).
type Backend interface {
	// Object store.
	Get(org, coll, key string) (val []byte, ok bool, err error)
	Put(org, coll, key string, val []byte) error
	Create(org, coll, key string, val []byte) error // ErrConflict if key exists
	Delete(org, coll, key string) (old []byte, existed bool, err error)
	Keys(org, coll string) ([]string, error)        // sorted
	Range(org, coll string, fn func(key string, raw []byte) bool) error
	Collections(org string) ([]string, error)       // non-empty collections, sorted

	// Blob store (cookbook file content).
	PutBlob(org, checksum string, data []byte) error
	Blob(org, checksum string) (data []byte, ok bool, err error)
	HasBlob(org, checksum string) (bool, error)
	DeleteBlob(org, checksum string) error

	// Org lifecycle.
	CreateOrg(name string) error // ErrConflict if org exists
	DeleteOrg(name string) (existed bool, err error)
	ListOrgs() ([]string, error) // named orgs only, sorted
	HasOrg(name string) (bool, error)

	Close() error
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/store/`
Expected: success (interface only, no implementation yet).

- [ ] **Step 3: Commit**

```bash
git add internal/store/backend.go
git commit -m "feat(store): define pluggable Backend interface"
```

---

### Task 2: Conformance suite

**Files:**
- Create: `internal/store/backendtest/backendtest.go`

**Interfaces:**
- Consumes: `store.Backend`.
- Produces: `backendtest.Run(t *testing.T, newBackend func(t *testing.T) store.Backend)` — the shared behavioral suite every backend must pass.

The suite is `package backendtest` (not a `_test.go`) so backend packages can import and
call it. It owns the contract: copy independence, sorted output, org namespacing,
global-space isolation, blob round-trip, org lifecycle, and data-drop on `DeleteOrg`.

- [ ] **Step 1: Write the suite**

```go
// Package backendtest is the shared conformance suite for store.Backend
// implementations. Each backend package calls Run from its own test.
package backendtest

import (
	"bytes"
	"testing"

	"github.com/tas50/cinc-zero/internal/store"
)

// Run exercises the full Backend contract against a fresh backend from newBackend.
func Run(t *testing.T, newBackend func(t *testing.T) store.Backend) {
	t.Helper()
	t.Run("ObjectRoundTrip", func(t *testing.T) { testObjectRoundTrip(t, newBackend(t)) })
	t.Run("CopyIndependence", func(t *testing.T) { testCopyIndependence(t, newBackend(t)) })
	t.Run("CreateConflict", func(t *testing.T) { testCreateConflict(t, newBackend(t)) })
	t.Run("DeleteSemantics", func(t *testing.T) { testDeleteSemantics(t, newBackend(t)) })
	t.Run("KeysSorted", func(t *testing.T) { testKeysSorted(t, newBackend(t)) })
	t.Run("CollectionsSorted", func(t *testing.T) { testCollectionsSorted(t, newBackend(t)) })
	t.Run("RangeEarlyStop", func(t *testing.T) { testRangeEarlyStop(t, newBackend(t)) })
	t.Run("OrgNamespacing", func(t *testing.T) { testOrgNamespacing(t, newBackend(t)) })
	t.Run("Blobs", func(t *testing.T) { testBlobs(t, newBackend(t)) })
	t.Run("OrgLifecycle", func(t *testing.T) { testOrgLifecycle(t, newBackend(t)) })
	t.Run("DeleteOrgDropsData", func(t *testing.T) { testDeleteOrgDropsData(t, newBackend(t)) })
}

func mustPut(t *testing.T, b store.Backend, org, coll, key, val string) {
	t.Helper()
	if err := b.Put(org, coll, key, []byte(val)); err != nil {
		t.Fatalf("Put(%q,%q,%q): %v", org, coll, key, err)
	}
}

func testObjectRoundTrip(t *testing.T, b store.Backend) {
	if _, ok, err := b.Get("acme", "nodes", "web"); err != nil || ok {
		t.Fatalf("missing Get: ok=%v err=%v", ok, err)
	}
	mustPut(t, b, "acme", "nodes", "web", `{"name":"web"}`)
	got, ok, err := b.Get("acme", "nodes", "web")
	if err != nil || !ok || string(got) != `{"name":"web"}` {
		t.Fatalf("Get after Put: got=%q ok=%v err=%v", got, ok, err)
	}
}

func testCopyIndependence(t *testing.T, b store.Backend) {
	mustPut(t, b, "acme", "nodes", "web", `{"a":1}`)
	got, _, _ := b.Get("acme", "nodes", "web")
	got[0] = 'X' // mutating the returned slice must not affect stored state
	again, _, _ := b.Get("acme", "nodes", "web")
	if string(again) != `{"a":1}` {
		t.Fatalf("stored value mutated via returned slice: %q", again)
	}
}

func testCreateConflict(t *testing.T, b store.Backend) {
	if err := b.Create("acme", "roles", "base", []byte(`{}`)); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if err := b.Create("acme", "roles", "base", []byte(`{}`)); err != store.ErrConflict {
		t.Fatalf("second Create: want ErrConflict, got %v", err)
	}
}

func testDeleteSemantics(t *testing.T, b store.Backend) {
	if _, existed, err := b.Delete("acme", "nodes", "ghost"); err != nil || existed {
		t.Fatalf("delete missing: existed=%v err=%v", existed, err)
	}
	mustPut(t, b, "acme", "nodes", "web", `{"name":"web"}`)
	old, existed, err := b.Delete("acme", "nodes", "web")
	if err != nil || !existed || string(old) != `{"name":"web"}` {
		t.Fatalf("delete existing: old=%q existed=%v err=%v", old, existed, err)
	}
	if _, ok, _ := b.Get("acme", "nodes", "web"); ok {
		t.Fatal("value present after delete")
	}
}

func testKeysSorted(t *testing.T, b store.Backend) {
	mustPut(t, b, "acme", "nodes", "c", `{}`)
	mustPut(t, b, "acme", "nodes", "a", `{}`)
	mustPut(t, b, "acme", "nodes", "b", `{}`)
	keys, err := b.Keys("acme", "nodes")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 || keys[0] != "a" || keys[1] != "b" || keys[2] != "c" {
		t.Fatalf("Keys not sorted: %v", keys)
	}
	empty, err := b.Keys("acme", "missing")
	if err != nil || len(empty) != 0 {
		t.Fatalf("Keys of empty coll: %v err=%v", empty, err)
	}
}

func testCollectionsSorted(t *testing.T, b store.Backend) {
	mustPut(t, b, "acme", "roles", "x", `{}`)
	mustPut(t, b, "acme", "nodes", "x", `{}`)
	// A collection emptied by Delete must not be reported.
	mustPut(t, b, "acme", "envs", "x", `{}`)
	b.Delete("acme", "envs", "x")
	colls, err := b.Collections("acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(colls) != 2 || colls[0] != "nodes" || colls[1] != "roles" {
		t.Fatalf("Collections: %v", colls)
	}
}

func testRangeEarlyStop(t *testing.T, b store.Backend) {
	for _, k := range []string{"a", "b", "c", "d"} {
		mustPut(t, b, "acme", "nodes", k, `{}`)
	}
	seen := 0
	err := b.Range("acme", "nodes", func(key string, raw []byte) bool {
		seen++
		return seen < 2 // stop after the second
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen != 2 {
		t.Fatalf("Range did not stop early: visited %d", seen)
	}
}

func testOrgNamespacing(t *testing.T, b store.Backend) {
	mustPut(t, b, "acme", "nodes", "web", `{"org":"acme"}`)
	mustPut(t, b, "other", "nodes", "web", `{"org":"other"}`)
	mustPut(t, b, "", "users", "pivotal", `{"global":true}`)
	a, _, _ := b.Get("acme", "nodes", "web")
	o, _, _ := b.Get("other", "nodes", "web")
	g, _, _ := b.Get("", "users", "pivotal")
	if string(a) != `{"org":"acme"}` || string(o) != `{"org":"other"}` || string(g) != `{"global":true}` {
		t.Fatalf("org namespaces leaked: a=%q o=%q g=%q", a, o, g)
	}
}

func testBlobs(t *testing.T, b store.Backend) {
	if ok, err := b.HasBlob("acme", "deadbeef"); err != nil || ok {
		t.Fatalf("HasBlob missing: ok=%v err=%v", ok, err)
	}
	if err := b.PutBlob("acme", "deadbeef", []byte("filecontent")); err != nil {
		t.Fatal(err)
	}
	got, ok, err := b.Blob("acme", "deadbeef")
	if err != nil || !ok || string(got) != "filecontent" {
		t.Fatalf("Blob: got=%q ok=%v err=%v", got, ok, err)
	}
	got[0] = 'X'
	again, _, _ := b.Blob("acme", "deadbeef")
	if !bytes.Equal(again, []byte("filecontent")) {
		t.Fatalf("blob mutated via returned slice: %q", again)
	}
	if err := b.DeleteBlob("acme", "deadbeef"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := b.HasBlob("acme", "deadbeef"); ok {
		t.Fatal("blob present after delete")
	}
}

func testOrgLifecycle(t *testing.T, b store.Backend) {
	if err := b.CreateOrg("acme"); err != nil {
		t.Fatal(err)
	}
	if err := b.CreateOrg("acme"); err != store.ErrConflict {
		t.Fatalf("CreateOrg conflict: want ErrConflict, got %v", err)
	}
	if err := b.CreateOrg("beta"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := b.HasOrg("acme"); !ok {
		t.Fatal("HasOrg acme should be true")
	}
	if ok, _ := b.HasOrg("nope"); ok {
		t.Fatal("HasOrg nope should be false")
	}
	orgs, err := b.ListOrgs()
	if err != nil || len(orgs) != 2 || orgs[0] != "acme" || orgs[1] != "beta" {
		t.Fatalf("ListOrgs: %v err=%v", orgs, err)
	}
	if existed, _ := b.DeleteOrg("acme"); !existed {
		t.Fatal("DeleteOrg acme should report existed")
	}
	if existed, _ := b.DeleteOrg("acme"); existed {
		t.Fatal("second DeleteOrg acme should report not-existed")
	}
}

func testDeleteOrgDropsData(t *testing.T, b store.Backend) {
	if err := b.CreateOrg("acme"); err != nil {
		t.Fatal(err)
	}
	mustPut(t, b, "acme", "nodes", "web", `{}`)
	if err := b.PutBlob("acme", "cafe", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.DeleteOrg("acme"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := b.Get("acme", "nodes", "web"); ok {
		t.Fatal("object survived DeleteOrg")
	}
	if ok, _ := b.HasBlob("acme", "cafe"); ok {
		t.Fatal("blob survived DeleteOrg")
	}
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/store/backendtest/`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add internal/store/backendtest/backendtest.go
git commit -m "test(store): shared Backend conformance suite"
```

---

### Task 3: Memory backend

**Files:**
- Create: `internal/store/memory/memory.go`
- Create: `internal/store/memory/memory_test.go`

**Interfaces:**
- Consumes: `store.Backend`, `store.ErrConflict`.
- Produces: `memory.New() *memory.Backend` (implements `store.Backend`).

- [ ] **Step 1: Write the failing test**

```go
package memory_test

import (
	"testing"

	"github.com/tas50/cinc-zero/internal/store"
	"github.com/tas50/cinc-zero/internal/store/backendtest"
	"github.com/tas50/cinc-zero/internal/store/memory"
)

func TestConformance(t *testing.T) {
	backendtest.Run(t, func(t *testing.T) store.Backend { return memory.New() })
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/store/memory/`
Expected: FAIL — `memory.New` undefined / package has no implementation.

- [ ] **Step 3: Implement the memory backend**

```go
// Package memory is the in-memory store.Backend: the default, ephemeral backend
// that keeps all state in maps. It is the reference implementation the conformance
// suite is written against.
package memory

import (
	"sort"
	"sync"

	"github.com/tas50/cinc-zero/internal/store"
)

// Backend is an in-memory store.Backend. The zero value is not usable; call New.
type Backend struct {
	mu    sync.RWMutex
	orgs  map[string]bool                      // named orgs (for ListOrgs/HasOrg)
	data  map[string]map[string]map[string][]byte // org -> coll -> key -> body
	blobs map[string]map[string][]byte         // org -> checksum -> content
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

// set stores a defensive copy; caller holds the write lock.
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/memory/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/memory/
git commit -m "feat(store): in-memory Backend implementation + conformance"
```

---

### Task 4: Add the `modernc.org/sqlite` dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Fetch the driver**

Run: `go get modernc.org/sqlite@latest`
Expected: `go.mod` gains `modernc.org/sqlite` and its transitive deps.

- [ ] **Step 2: Verify the build still works**

Run: `go build ./... && go vet ./...`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "build: add modernc.org/sqlite (pure-Go) dependency"
```

---

### Task 5: SQLite backend

**Files:**
- Create: `internal/store/sqlite/sqlite.go`
- Create: `internal/store/sqlite/sqlite_test.go`

**Interfaces:**
- Consumes: `store.Backend`, `store.ErrConflict`.
- Produces: `sqlite.Open(dsn string) (*sqlite.Backend, error)` (implements `store.Backend`).
  `dsn` is a file path; `:memory:` / `file::memory:?cache=shared` is allowed for tests.

Schema (created on Open if absent), bodies opaque:

```sql
CREATE TABLE IF NOT EXISTS objects (
  org TEXT NOT NULL, collection TEXT NOT NULL, key TEXT NOT NULL,
  body BLOB NOT NULL, PRIMARY KEY (org, collection, key));
CREATE TABLE IF NOT EXISTS blobs (
  org TEXT NOT NULL, checksum TEXT NOT NULL, content BLOB NOT NULL,
  PRIMARY KEY (org, checksum));
CREATE TABLE IF NOT EXISTS orgs (name TEXT PRIMARY KEY);
CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER NOT NULL);
```

- [ ] **Step 1: Write the failing test**

```go
package sqlite_test

import (
	"path/filepath"
	"testing"

	"github.com/tas50/cinc-zero/internal/store"
	"github.com/tas50/cinc-zero/internal/store/backendtest"
	"github.com/tas50/cinc-zero/internal/store/sqlite"
)

func TestConformance(t *testing.T) {
	backendtest.Run(t, func(t *testing.T) store.Backend {
		db := filepath.Join(t.TempDir(), "test.db")
		b, err := sqlite.Open(db)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(func() { b.Close() })
		return b
	})
}

func TestPersistsAcrossReopen(t *testing.T) {
	db := filepath.Join(t.TempDir(), "p.db")
	b, err := sqlite.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Put("acme", "nodes", "web", []byte(`{"n":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	b2, err := sqlite.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer b2.Close()
	got, ok, err := b2.Get("acme", "nodes", "web")
	if err != nil || !ok || string(got) != `{"n":1}` {
		t.Fatalf("after reopen: got=%q ok=%v err=%v", got, ok, err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/store/sqlite/`
Expected: FAIL — `sqlite.Open` undefined.

- [ ] **Step 3: Implement the sqlite backend**

```go
// Package sqlite is the durable store.Backend backed by SQLite via the pure-Go
// modernc.org/sqlite driver (no cgo). Object bodies and blob contents are stored
// opaquely, so the schema is invariant to Chef object shapes.
package sqlite

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/tas50/cinc-zero/internal/store"
	_ "modernc.org/sqlite"
)

const schemaVersion = 1

// Backend is a SQLite-backed store.Backend.
type Backend struct {
	db *sql.DB
}

// Open opens (creating if needed) a SQLite database at dsn and applies migrations.
func Open(dsn string) (*Backend, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// WAL + busy_timeout for safe concurrent single-process access.
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma %q: %w", pragma, err)
		}
	}
	b := &Backend{db: db}
	if err := b.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return b, nil
}

func (b *Backend) migrate() error {
	const ddl = `
CREATE TABLE IF NOT EXISTS objects (
  org TEXT NOT NULL, collection TEXT NOT NULL, key TEXT NOT NULL,
  body BLOB NOT NULL, PRIMARY KEY (org, collection, key));
CREATE TABLE IF NOT EXISTS blobs (
  org TEXT NOT NULL, checksum TEXT NOT NULL, content BLOB NOT NULL,
  PRIMARY KEY (org, checksum));
CREATE TABLE IF NOT EXISTS orgs (name TEXT PRIMARY KEY);
CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER NOT NULL);`
	if _, err := b.db.Exec(ddl); err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	var v sql.NullInt64
	if err := b.db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		return err
	}
	if !v.Valid {
		if _, err := b.db.Exec(`INSERT INTO schema_migrations(version) VALUES (?)`, schemaVersion); err != nil {
			return err
		}
	}
	return nil
}

func (b *Backend) Get(org, coll, key string) ([]byte, bool, error) {
	var body []byte
	err := b.db.QueryRow(
		`SELECT body FROM objects WHERE org=? AND collection=? AND key=?`,
		org, coll, key).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return body, true, nil
}

func (b *Backend) Put(org, coll, key string, val []byte) error {
	_, err := b.db.Exec(
		`INSERT INTO objects(org,collection,key,body) VALUES(?,?,?,?)
		 ON CONFLICT(org,collection,key) DO UPDATE SET body=excluded.body`,
		org, coll, key, val)
	return err
}

func (b *Backend) Create(org, coll, key string, val []byte) error {
	res, err := b.db.Exec(
		`INSERT INTO objects(org,collection,key,body) VALUES(?,?,?,?)
		 ON CONFLICT(org,collection,key) DO NOTHING`,
		org, coll, key, val)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return store.ErrConflict
	}
	return nil
}

func (b *Backend) Delete(org, coll, key string) ([]byte, bool, error) {
	old, ok, err := b.Get(org, coll, key)
	if err != nil || !ok {
		return nil, false, err
	}
	if _, err := b.db.Exec(
		`DELETE FROM objects WHERE org=? AND collection=? AND key=?`,
		org, coll, key); err != nil {
		return nil, false, err
	}
	return old, true, nil
}

func (b *Backend) Keys(org, coll string) ([]string, error) {
	rows, err := b.db.Query(
		`SELECT key FROM objects WHERE org=? AND collection=? ORDER BY key`, org, coll)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (b *Backend) Range(org, coll string, fn func(key string, raw []byte) bool) error {
	rows, err := b.db.Query(
		`SELECT key, body FROM objects WHERE org=? AND collection=? ORDER BY key`, org, coll)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var body []byte
		if err := rows.Scan(&k, &body); err != nil {
			return err
		}
		if !fn(k, body) {
			return nil
		}
	}
	return rows.Err()
}

func (b *Backend) Collections(org string) ([]string, error) {
	rows, err := b.db.Query(
		`SELECT DISTINCT collection FROM objects WHERE org=? ORDER BY collection`, org)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var colls []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		colls = append(colls, c)
	}
	return colls, rows.Err()
}

func (b *Backend) PutBlob(org, checksum string, data []byte) error {
	_, err := b.db.Exec(
		`INSERT INTO blobs(org,checksum,content) VALUES(?,?,?)
		 ON CONFLICT(org,checksum) DO UPDATE SET content=excluded.content`,
		org, checksum, data)
	return err
}

func (b *Backend) Blob(org, checksum string) ([]byte, bool, error) {
	var content []byte
	err := b.db.QueryRow(
		`SELECT content FROM blobs WHERE org=? AND checksum=?`, org, checksum).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return content, true, nil
}

func (b *Backend) HasBlob(org, checksum string) (bool, error) {
	var one int
	err := b.db.QueryRow(
		`SELECT 1 FROM blobs WHERE org=? AND checksum=?`, org, checksum).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (b *Backend) DeleteBlob(org, checksum string) error {
	_, err := b.db.Exec(`DELETE FROM blobs WHERE org=? AND checksum=?`, org, checksum)
	return err
}

func (b *Backend) CreateOrg(name string) error {
	res, err := b.db.Exec(`INSERT INTO orgs(name) VALUES(?) ON CONFLICT(name) DO NOTHING`, name)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return store.ErrConflict
	}
	return nil
}

func (b *Backend) DeleteOrg(name string) (bool, error) {
	res, err := b.db.Exec(`DELETE FROM orgs WHERE name=?`, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	if _, err := b.db.Exec(`DELETE FROM objects WHERE org=?`, name); err != nil {
		return false, err
	}
	if _, err := b.db.Exec(`DELETE FROM blobs WHERE org=?`, name); err != nil {
		return false, err
	}
	return true, nil
}

func (b *Backend) ListOrgs() ([]string, error) {
	rows, err := b.db.Query(`SELECT name FROM orgs ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

func (b *Backend) HasOrg(name string) (bool, error) {
	var one int
	err := b.db.QueryRow(`SELECT 1 FROM orgs WHERE name=?`, name).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (b *Backend) Close() error { return b.db.Close() }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/sqlite/ -race`
Expected: PASS (conformance + reopen).

- [ ] **Step 5: Commit**

```bash
git add internal/store/sqlite/
git commit -m "feat(store): SQLite Backend (modernc.org/sqlite) + conformance"
```

---

### Task 6: Full-suite + vet gate

- [ ] **Step 1: Run the whole suite**

Run: `make test && make vet`
Expected: PASS — no existing test disturbed (the layer is not yet wired in).

- [ ] **Step 2: Commit any fmt fixes**

```bash
make fmt && git add -A && git commit -m "chore: gofmt" || true
```

---

## PR 2 (task-level; expanded to steps when PR 1 merges)

### Task 7: Add `Tx` to `Backend` + both implementations
- Add `Tx(fn func(Backend) error) error` to the interface. Memory: snapshot-on-begin
  overlay applied atomically on success, discarded on error. SQLite: native
  `BEGIN`/`COMMIT`/`ROLLBACK` wrapping a `*Backend` bound to the `*sql.Tx`.
- Conformance test: a `Tx` that errors rolls back all writes; one that returns nil commits.

### Task 8: `Store`/`Org` delegate to a `Backend`; propagate `error`
- `store.New()` → memory backend; `store.NewWithBackend(b Backend)`.
- `Org` becomes `{backend Backend, name string}`. Methods return `error`.
- `Store.Global()` → org `""`. `View`/`BlobView` map to `Get`/`Blob` (copy is acceptable;
  the no-copy optimization stays memory-only and internal).
- Update all ~174 non-test call sites across `internal/api`, `server`, `repo`, `state`
  to handle the new error, surfacing DB failures as JSON `500` via `writeError`.
- Drive compiler-first: change signatures, `go build ./...`, fix each break, keep the
  existing tests green against the memory backend.

### Task 9: Flags + `server.Options` wiring
- `cmd/cinc-zero`: `--storage memory|sqlite` (default memory), `--db PATH`; env
  `CINC_ZERO_STORAGE`, `CINC_ZERO_DB`. `--storage sqlite` without `--db` is an error.
- `server.Options`: `Storage string`, `DB string`, optional injected `Backend`. Zero value
  stays memory. `server.New` opens the sqlite backend when selected and `Close`s it on `Stop`.
- Test: `startServer` parametrized over backend; a node created, server stopped and a new
  server opened on the same DB file still serves the node.

### Task 10: Atomic org create/delete
- Wrap `api.CreateOrganization*` and org deletion in `backend.Tx`.

## PR 3 (task-level)

### Task 11: Docs
- `CLAUDE.md`/README: `--storage`/`--db`, container example (`-v vol:/data --storage sqlite
  --db /data/cinc.db`), backup recipes (sqlite `VACUUM INTO`; future `pg_dump`/RDS snapshot),
  forward-only migration/upgrade note. Update the README status table for storage.

### Task 12 (optional): `Searcher` capability seam
- Optional interface a backend may implement to push search predicates down (sqlite JSON1,
  Postgres `jsonb`+GIN); the engine probes for it and falls back to the `Range` scan.

---

## Self-Review

- **Spec coverage:** Backend interface (T1), memory (T3), sqlite + schema + migrations + pure-Go
  driver (T4/T5), conformance suite (T2), persistence-across-reopen (T5), error propagation +
  facade + flags (T8/T9), Tx atomicity (T7/T10), backups/upgrades/container docs (T11), search
  seam (T12). All spec sections map to a task.
- **Placeholders:** none — PR 1 tasks carry full code; PR 2/3 are intentionally task-level and
  flagged for step-expansion at merge.
- **Type consistency:** `Backend` method signatures in T1 match their use in T2 (suite), T3
  (memory), and T5 (sqlite); `Create`/`CreateOrg` return `error` (ErrConflict), `Delete`/`DeleteOrg`
  return `(…, bool, error)` consistently across suite and both backends.
