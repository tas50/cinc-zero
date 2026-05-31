package store

import (
	"bytes"
	"testing"
)

func TestBlobStore(t *testing.T) {
	st := New()
	org, err := st.CreateOrg("acme")
	if err != nil {
		t.Fatal(err)
	}

	if org.HasBlob("abc123") {
		t.Fatal("blob should not exist yet")
	}
	if _, ok := org.Blob("abc123"); ok {
		t.Fatal("Blob returned ok for missing checksum")
	}

	content := []byte("file content\n")
	org.PutBlob("abc123", content)

	if !org.HasBlob("abc123") {
		t.Fatal("blob should exist after PutBlob")
	}
	got, ok := org.Blob("abc123")
	if !ok || !bytes.Equal(got, content) {
		t.Fatalf("Blob = %q, %v; want %q", got, ok, content)
	}

	// Stored value is a defensive copy: mutating the input must not change it.
	content[0] = 'X'
	got, _ = org.Blob("abc123")
	if got[0] == 'X' {
		t.Fatal("PutBlob did not copy input")
	}
	// And mutating the returned slice must not change the stored value.
	got[1] = 'Y'
	again, _ := org.Blob("abc123")
	if again[1] == 'Y' {
		t.Fatal("Blob did not return a copy")
	}

	// Blobs are org-scoped.
	other, _ := st.CreateOrg("other")
	if other.HasBlob("abc123") {
		t.Fatal("blobs leaked across orgs")
	}
}

func TestBlobViewReturnsReferenceNotCopy(t *testing.T) {
	st := New()
	org, _ := st.CreateOrg("acme")
	org.PutBlob("abc123", []byte("file content\n"))

	got, ok := org.BlobView("abc123")
	if !ok || string(got) != "file content\n" {
		t.Fatalf("BlobView = %q, %v", got, ok)
	}
	if _, ok := org.BlobView("missing"); ok {
		t.Fatal("BlobView of missing checksum should report false")
	}

	// BlobView returns the backing slice directly; Blob copies.
	a, _ := org.BlobView("abc123")
	b, _ := org.BlobView("abc123")
	if &a[0] != &b[0] {
		t.Fatal("BlobView should return the backing slice without copying")
	}
	c, _ := org.Blob("abc123")
	if &a[0] == &c[0] {
		t.Fatal("Blob should return a defensive copy distinct from BlobView")
	}
}
