package search

import (
	"encoding/json"
	"slices"
	"testing"
)

func mustDoc(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestFlatten(t *testing.T) {
	doc := mustDoc(t, `{
		"name": "web01",
		"tags": ["prod", "frontend"],
		"foo": {"bar": {"baz": "qux"}},
		"count": 3,
		"enabled": true
	}`)
	fields := Flatten(doc)

	cases := []struct {
		key  string
		want []string
	}{
		{"name", []string{"web01"}},
		{"tags", []string{"prod", "frontend"}},
		{"foo_bar_baz", []string{"qux"}}, // full path
		{"bar_baz", []string{"qux"}},     // suffix
		{"baz", []string{"qux"}},         // deepest suffix
		{"count", []string{"3"}},
		{"enabled", []string{"true"}},
	}
	for _, c := range cases {
		got := fields[c.key]
		for _, w := range c.want {
			if !slices.Contains(got, w) {
				t.Errorf("fields[%q] = %v, want to contain %q", c.key, got, w)
			}
		}
	}
}

func TestFlattenLowercases(t *testing.T) {
	fields := Flatten(mustDoc(t, `{"Role": "WebServer"}`))
	if !slices.Contains(fields["role"], "webserver") {
		t.Fatalf("expected lowercased key and value, got %v", fields)
	}
}
