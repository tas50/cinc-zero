package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// These tests pin behavior that previously regressed. Each corresponds to a
// confirmed bug fixed in the same change; the comment names the symptom so a
// future regression is easy to trace back.

// TestSearchPaginationOverflowNoPanic guards against an integer overflow in the
// search result window. A caller-supplied start near math.MaxInt made
// start+rows wrap negative, producing an invalid (low > high) slice that
// panicked the handler. A huge start must instead yield an empty window.
func TestSearchPaginationOverflowNoPanic(t *testing.T) {
	srv, st := newTestAPI(t)
	org, _ := st.Org("acme")
	org.Put("nodes", "n1", []byte(`{"name":"n1"}`))

	resp, body := do(t, "GET",
		srv.URL+"/organizations/acme/search/node?q=*:*&start=9223372036854775807&rows=1000", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("overflowing start = %d, want 200; body %s", resp.StatusCode, body)
	}
	var res struct {
		Total int               `json:"total"`
		Rows  []json.RawMessage `json:"rows"`
	}
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatalf("decode search response: %v (%s)", err, body)
	}
	if res.Total != 1 {
		t.Errorf("total = %d, want 1", res.Total)
	}
	if len(res.Rows) != 0 {
		t.Errorf("rows = %d, want 0 (start past the end)", len(res.Rows))
	}
}

// TestSearchPaginationWindow checks ordinary start/rows windowing still works,
// so the overflow guard did not change the happy path.
func TestSearchPaginationWindow(t *testing.T) {
	srv, st := newTestAPI(t)
	org, _ := st.Org("acme")
	for _, id := range []string{"a", "b", "c", "d"} {
		org.Put("nodes", id, []byte(`{"name":"`+id+`"}`))
	}
	_, body := do(t, "GET", srv.URL+"/organizations/acme/search/node?q=*:*&start=1&rows=2", "")
	var res struct {
		Total int `json:"total"`
		Start int `json:"start"`
		Rows  []struct {
			Name string `json:"name"`
		} `json:"rows"`
	}
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if res.Total != 4 {
		t.Errorf("total = %d, want 4", res.Total)
	}
	if len(res.Rows) != 2 || res.Rows[0].Name != "b" || res.Rows[1].Name != "c" {
		t.Errorf("window = %+v, want [b c]", res.Rows)
	}
}

// TestGlobalUserURIShape pins that global users (which are not org-scoped) are
// addressed at the top level. The URI builder previously prefixed every object
// with /organizations/{org}/, producing the malformed /organizations//users/...
// for the empty org of a global user.
func TestGlobalUserURIShape(t *testing.T) {
	srv, _ := newTestAPI(t)

	resp, body := do(t, "POST", srv.URL+"/users", `{"name":"alice"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create user = %d: %s", resp.StatusCode, body)
	}
	var created struct {
		URI string `json:"uri"`
	}
	json.Unmarshal([]byte(body), &created)
	if !strings.HasSuffix(created.URI, "/users/alice") || strings.Contains(created.URI, "/organizations/") {
		t.Errorf("create uri = %q, want it to end in /users/alice with no /organizations/ segment", created.URI)
	}

	_, body = do(t, "GET", srv.URL+"/users", "")
	var list map[string]string
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("decode user list: %v (%s)", err, body)
	}
	if got := list["alice"]; !strings.HasSuffix(got, "/users/alice") || strings.Contains(got, "/organizations/") {
		t.Errorf("list uri = %q, want it to end in /users/alice with no /organizations/ segment", got)
	}
}

// TestGlobalUserKeyURIShape pins the same top-level addressing for a global
// user's key URIs, which are derived from the same builder.
func TestGlobalUserKeyURIShape(t *testing.T) {
	srv, _ := newTestAPI(t)
	if resp, body := do(t, "POST", srv.URL+"/users", `{"name":"alice"}`); resp.StatusCode != http.StatusCreated {
		t.Fatalf("create user = %d: %s", resp.StatusCode, body)
	}
	_, body := do(t, "GET", srv.URL+"/users/alice/keys", "")
	var list []keyListEntry
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("decode key list: %v (%s)", err, body)
	}
	for _, k := range list {
		if strings.Contains(k.URI, "/organizations/") || !strings.Contains(k.URI, "/users/alice/keys/") {
			t.Errorf("key %q uri = %q, want a top-level /users/alice/keys/... URL", k.Name, k.URI)
		}
	}
}

// TestAssociateUserJoinsUsersGroup pins that direct association
// (POST /organizations/{org}/users) makes the user an org member by adding them
// to the org's "users" group — the same effect invite acceptance has. Without
// it, directly-associated users lacked group-based permissions under ACL
// enforcement.
func TestAssociateUserJoinsUsersGroup(t *testing.T) {
	srv, st := newTestAPI(t)
	org, _ := st.Org("acme")
	seedAuthz(org)
	st.Global().Put("users", "alice", []byte(`{"username":"alice"}`))

	if resp, body := do(t, "POST", srv.URL+"/organizations/acme/users", `{"username":"alice"}`); resp.StatusCode != http.StatusCreated {
		t.Fatalf("associate = %d: %s", resp.StatusCode, body)
	}

	_, body := do(t, "GET", srv.URL+"/organizations/acme/groups/users", "")
	var g struct {
		Users []string `json:"users"`
	}
	if err := json.Unmarshal([]byte(body), &g); err != nil {
		t.Fatalf("decode users group: %v (%s)", err, body)
	}
	found := false
	for _, u := range g.Users {
		if u == "alice" {
			found = true
		}
	}
	if !found {
		t.Errorf("users group = %v, want it to contain alice", g.Users)
	}
}

// TestCreateDataBagStoresCanonicalJSON pins that a bag name is stored as
// canonical JSON. The name was previously concatenated into a JSON string
// literal, so a name containing a double quote produced a malformed,
// unparseable stored value.
func TestCreateDataBagStoresCanonicalJSON(t *testing.T) {
	srv, st := newTestAPI(t)
	org, _ := st.Org("acme")

	const name = `a"b`
	if resp, body := do(t, "POST", srv.URL+"/organizations/acme/data", `{"name":"a\"b"}`); resp.StatusCode != http.StatusCreated {
		t.Fatalf("create data bag = %d: %s", resp.StatusCode, body)
	}

	raw, ok := org.Get(dataBagsColl, name)
	if !ok {
		t.Fatalf("data bag %q not stored", name)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("stored data bag is not valid JSON: %v (%s)", err, raw)
	}
	if doc["name"] != name {
		t.Errorf("stored name = %v, want %q", doc["name"], name)
	}
}
