package api

import (
	"encoding/json"
	"strings"
	"testing"
)

// seedNode stores a node with the given name, environment, and a normal
// attribute foo.bar so search can exercise nested-attribute matching.
func seedNode(t *testing.T, base, name, env, bar string) {
	t.Helper()
	body := `{"name":"` + name + `","chef_environment":"` + env + `","json_class":"Chef::Node",` +
		`"normal":{"foo":{"bar":"` + bar + `"}},"run_list":["recipe[nginx]"]}`
	resp, b := do(t, "PUT", base+"/nodes/"+name, body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		t.Fatalf("seed node %s = %d: %s", name, resp.StatusCode, b)
	}
}

type searchResult struct {
	Total int               `json:"total"`
	Start int               `json:"start"`
	Rows  []json.RawMessage `json:"rows"`
}

func TestSearchNodes(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/organizations/acme"
	seedNode(t, base, "web01", "production", "alpha")
	seedNode(t, base, "web02", "production", "beta")
	seedNode(t, base, "db01", "staging", "alpha")

	// Match all.
	var res searchResult
	_, body := do(t, "GET", base+"/search/node?q=*:*", "")
	json.Unmarshal([]byte(body), &res)
	if res.Total != 3 {
		t.Fatalf("q=*:* total = %d, want 3: %s", res.Total, body)
	}

	// Field match on a top-level attribute.
	_, body = do(t, "GET", base+"/search/node?q=chef_environment:production", "")
	json.Unmarshal([]byte(body), &res)
	if res.Total != 2 {
		t.Fatalf("env:production total = %d, want 2", res.Total)
	}

	// Nested attribute via deepest suffix key.
	_, body = do(t, "GET", base+"/search/node?q=foo_bar:alpha", "")
	json.Unmarshal([]byte(body), &res)
	if res.Total != 2 {
		t.Fatalf("foo_bar:alpha total = %d, want 2: %s", res.Total, body)
	}

	// Boolean AND.
	_, body = do(t, "GET", base+"/search/node?q=chef_environment:production+AND+foo_bar:alpha", "")
	json.Unmarshal([]byte(body), &res)
	if res.Total != 1 {
		t.Fatalf("AND query total = %d, want 1: %s", res.Total, body)
	}
}

func TestSearchPagination(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/organizations/acme"
	for _, n := range []string{"a", "b", "c", "d"} {
		seedNode(t, base, n, "production", "x")
	}

	var res searchResult
	_, body := do(t, "GET", base+"/search/node?q=*:*&start=1&rows=2", "")
	json.Unmarshal([]byte(body), &res)
	if res.Total != 4 || len(res.Rows) != 2 || res.Start != 1 {
		t.Fatalf("pagination = total %d start %d rows %d: %s", res.Total, res.Start, len(res.Rows), body)
	}
}

func TestSearchDataBag(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/organizations/acme"
	do(t, "POST", base+"/data", `{"name":"users"}`)
	do(t, "POST", base+"/data/users", `{"id":"alice","admin":true}`)
	do(t, "POST", base+"/data/users", `{"id":"bob","admin":false}`)

	// The data bag appears as a search index.
	_, body := do(t, "GET", base+"/search", "")
	var indexes map[string]string
	json.Unmarshal([]byte(body), &indexes)
	if _, ok := indexes["users"]; !ok {
		t.Fatalf("data bag index missing: %s", body)
	}

	var res searchResult
	_, body = do(t, "GET", base+"/search/users?q=admin:true", "")
	json.Unmarshal([]byte(body), &res)
	if res.Total != 1 {
		t.Fatalf("data bag search total = %d, want 1: %s", res.Total, body)
	}
}

func TestPartialSearch(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/organizations/acme"
	seedNode(t, base, "web01", "production", "alpha")

	// Project chef_environment and the nested foo.bar attribute.
	_, body := do(t, "POST", base+"/search/node?q=name:web01",
		`{"env":["chef_environment"],"bar":["foo","bar"]}`)
	var res struct {
		Total int `json:"total"`
		Rows  []struct {
			URL  string         `json:"url"`
			Data map[string]any `json:"data"`
		} `json:"rows"`
	}
	json.Unmarshal([]byte(body), &res)
	if res.Total != 1 || len(res.Rows) != 1 {
		t.Fatalf("partial search total = %d: %s", res.Total, body)
	}
	row := res.Rows[0]
	if row.Data["env"] != "production" || row.Data["bar"] != "alpha" {
		t.Fatalf("partial data = %+v", row.Data)
	}
	if !strings.HasSuffix(row.URL, "/nodes/web01") {
		t.Fatalf("partial url = %s", row.URL)
	}
}

func TestSearchUnknownIndex404(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/organizations/acme"
	resp, _ := do(t, "GET", base+"/search/nonexistent", "")
	if resp.StatusCode != 404 {
		t.Fatalf("unknown index = %d, want 404", resp.StatusCode)
	}
}

func TestSearchBadQuery400(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/organizations/acme"
	resp, _ := do(t, "GET", base+"/search/node?q=%28unclosed", "")
	if resp.StatusCode != 400 {
		t.Fatalf("bad query = %d, want 400", resp.StatusCode)
	}
}
