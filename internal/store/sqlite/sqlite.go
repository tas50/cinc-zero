// Package sqlite is the durable store.Backend backed by SQLite via the pure-Go
// modernc.org/sqlite driver (no cgo). Object bodies and blob contents are stored
// opaquely, so the schema is invariant to Chef object shapes and a single static
// binary can persist all server state to a file.
package sqlite

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/tas50/cinc-zero/internal/store"
	_ "modernc.org/sqlite"
)

// schemaVersion is the current on-disk schema revision recorded in
// schema_migrations. Migrations are forward-only; bump this and extend migrate
// when the schema changes.
const schemaVersion = 1

// Backend is a SQLite-backed store.Backend.
type Backend struct {
	db *sql.DB
}

// Open opens (creating if needed) a SQLite database at dsn and applies migrations.
// dsn is a file path; ":memory:" yields an ephemeral database (mainly for tests).
func Open(dsn string) (*Backend, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// A single *sql.DB pools connections; SQLite write-serialization plus WAL and
	// a busy timeout keep concurrent access safe within one process.
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

// migrate creates the schema if absent and records the schema version. It is
// forward-only and safe to run on every Open.
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
