package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/tas50/cinc-zero/internal/store"
)

func seededServer(t *testing.T) *httptest.Server {
	t.Helper()
	st := store.New()
	org, _ := st.CreateOrg("acme")
	SeedOrg(org)
	srv := httptest.NewServer(New(st).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func TestDefaultGroupsSeeded(t *testing.T) {
	srv := seededServer(t)
	base := srv.URL + "/organizations/acme"

	_, body := do(t, "GET", base+"/groups", "")
	var groups map[string]string
	json.Unmarshal([]byte(body), &groups)
	for _, want := range []string{"admins", "clients", "users"} {
		if _, ok := groups[want]; !ok {
			t.Fatalf("default group %q missing: %s", want, body)
		}
	}
}

func TestGroupCRUD(t *testing.T) {
	srv := seededServer(t)
	base := srv.URL + "/organizations/acme"

	resp, _ := do(t, "POST", base+"/groups", `{"name":"ops","groupname":"ops"}`)
	if resp.StatusCode != 201 {
		t.Fatalf("create group = %d", resp.StatusCode)
	}
	resp, _ = do(t, "PUT", base+"/groups/ops", `{"name":"ops","actors":{"users":["alice"],"clients":[],"groups":[]}}`)
	if resp.StatusCode != 200 {
		t.Fatalf("update group = %d", resp.StatusCode)
	}
	resp, body := do(t, "GET", base+"/groups/ops", "")
	if resp.StatusCode != 200 {
		t.Fatalf("get group = %d", resp.StatusCode)
	}
	if !json.Valid([]byte(body)) {
		t.Fatalf("group doc invalid: %s", body)
	}
}

func TestDefaultContainersSeeded(t *testing.T) {
	srv := seededServer(t)
	base := srv.URL + "/organizations/acme"

	_, body := do(t, "GET", base+"/containers", "")
	var containers map[string]string
	json.Unmarshal([]byte(body), &containers)
	for _, want := range []string{"nodes", "roles", "environments", "cookbooks", "data"} {
		if _, ok := containers[want]; !ok {
			t.Fatalf("default container %q missing: %s", want, body)
		}
	}
}

func TestContainerGet(t *testing.T) {
	srv := seededServer(t)
	base := srv.URL + "/organizations/acme"
	resp, body := do(t, "GET", base+"/containers/nodes", "")
	if resp.StatusCode != 200 {
		t.Fatalf("get container = %d", resp.StatusCode)
	}
	var c map[string]any
	json.Unmarshal([]byte(body), &c)
	if c["containername"] != "nodes" {
		t.Fatalf("container doc = %s", body)
	}
}
