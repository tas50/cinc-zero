package main

import (
	"encoding/json"
	"testing"
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
