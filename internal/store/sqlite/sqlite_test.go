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
