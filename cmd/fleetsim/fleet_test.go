package main

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestSelectStuckCountAndDeterminism(t *testing.T) {
	var names []string
	for i := range 100 {
		names = append(names, fmt.Sprintf("node%02d", i))
	}
	a := selectStuck(names, 0.02, rand.New(rand.NewSource(7)))
	if len(a) != 2 {
		t.Fatalf("got %d stuck, want 2 (ceil(0.02*100))", len(a))
	}
	b := selectStuck(names, 0.02, rand.New(rand.NewSource(7)))
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("same seed produced different sets: %v vs %v", a, b)
	}
	c := selectStuck(names, 0.02, rand.New(rand.NewSource(8)))
	if reflect.DeepEqual(a, c) {
		t.Fatalf("different seeds produced identical sets")
	}
}

func TestSelectStuckCeil(t *testing.T) {
	// 0.02 * 3 = 0.06 -> ceil = 1
	got := selectStuck([]string{"a", "b", "c"}, 0.02, rand.New(rand.NewSource(1)))
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
}

func TestDiscover(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /nodes", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"n1":"http://x/nodes/n1","n2":"http://x/nodes/n2"}`))
	})
	mux.HandleFunc("GET /nodes/{name}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"name":%q}`, r.PathValue("name"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, _ := newClient(srv.URL, "", "", 5*time.Second)
	nodes, err := discover(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || nodes[0].name != "n1" || nodes[1].name != "n2" {
		t.Fatalf("discover = %+v", nodes)
	}
}
