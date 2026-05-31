package api

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/tas50/cinc-zero/internal/store"
)

// BenchmarkListObjects measures the node-list endpoint end-to-end through the
// router for a large collection: key collection + URL building + JSON encoding.
func BenchmarkListObjects(b *testing.B) {
	st := store.New()
	org, _ := st.CreateOrg("acme")
	a := New(st)
	for i := range 500 {
		org.Put("nodes", fmt.Sprintf("node%d", i), []byte(`{"name":"x"}`))
	}
	h := a.Handler()
	req := httptest.NewRequest("GET", "http://127.0.0.1/organizations/acme/nodes", nil)

	b.ReportAllocs()
	for b.Loop() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			b.Fatalf("status %d", rec.Code)
		}
	}
}
