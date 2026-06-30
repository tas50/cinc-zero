package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStampOhaiTimeSetsTimestamp(t *testing.T) {
	out, err := stampOhaiTime([]byte(`{"name":"n1","automatic":{"ohai_time":1.0,"fqdn":"n1.example.com"}}`), 1780000000)
	if err != nil {
		t.Fatal(err)
	}
	var node map[string]any
	json.Unmarshal(out, &node)
	auto := node["automatic"].(map[string]any)
	if auto["ohai_time"].(float64) != 1780000000 {
		t.Fatalf("ohai_time = %v, want 1780000000", auto["ohai_time"])
	}
	if auto["fqdn"] != "n1.example.com" {
		t.Fatalf("clobbered fqdn: %v", auto["fqdn"])
	}
	if node["name"] != "n1" {
		t.Fatalf("clobbered name: %v", node["name"])
	}
}

func TestStampOhaiTimeCreatesAutomatic(t *testing.T) {
	out, err := stampOhaiTime([]byte(`{"name":"n1"}`), 42)
	if err != nil {
		t.Fatal(err)
	}
	var node map[string]any
	json.Unmarshal(out, &node)
	auto, ok := node["automatic"].(map[string]any)
	if !ok || auto["ohai_time"].(float64) != 42 {
		t.Fatalf("automatic not created: %v", node)
	}
}

func TestCheckInStampsAndPuts(t *testing.T) {
	var putBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("GET /nodes/n1", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name":"n1","automatic":{"ohai_time":1.0}}`))
	})
	mux.HandleFunc("PUT /nodes/n1", func(w http.ResponseWriter, r *http.Request) {
		putBody, _ = io.ReadAll(r.Body)
		w.Write([]byte(`{"name":"n1"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := newClient(srv.URL, "", "", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	n := &node{name: "n1", body: []byte(`{"name":"n1"}`)}
	if err := c.checkIn(n, 1780000000); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	json.Unmarshal(putBody, &got)
	auto := got["automatic"].(map[string]any)
	if auto["ohai_time"].(float64) != 1780000000 {
		t.Fatalf("PUT ohai_time = %v, want 1780000000", auto["ohai_time"])
	}
}
