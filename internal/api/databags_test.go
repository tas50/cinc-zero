package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDataBagLifecycle(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/organizations/acme"

	// Empty bag list.
	resp, body := do(t, "GET", base+"/data", "")
	if resp.StatusCode != 200 || strings.TrimSpace(body) != "{}" {
		t.Fatalf("empty data list = %d %s", resp.StatusCode, body)
	}

	// Create a bag.
	resp, body = do(t, "POST", base+"/data", `{"name":"secrets"}`)
	if resp.StatusCode != 201 {
		t.Fatalf("create bag = %d: %s", resp.StatusCode, body)
	}

	// Bag appears in the list.
	_, body = do(t, "GET", base+"/data", "")
	var bags map[string]string
	json.Unmarshal([]byte(body), &bags)
	if !strings.HasSuffix(bags["secrets"], "/data/secrets") {
		t.Fatalf("bag list = %s", body)
	}

	// Empty item list for the bag.
	resp, body = do(t, "GET", base+"/data/secrets", "")
	if resp.StatusCode != 200 || strings.TrimSpace(body) != "{}" {
		t.Fatalf("empty item list = %d %s", resp.StatusCode, body)
	}

	// Create an item (keyed by "id").
	resp, body = do(t, "POST", base+"/data/secrets", `{"id":"db_password","value":"hunter2"}`)
	if resp.StatusCode != 201 {
		t.Fatalf("create item = %d: %s", resp.StatusCode, body)
	}

	// Get the item back.
	resp, body = do(t, "GET", base+"/data/secrets/db_password", "")
	if resp.StatusCode != 200 {
		t.Fatalf("get item = %d", resp.StatusCode)
	}
	var item map[string]any
	json.Unmarshal([]byte(body), &item)
	if item["value"] != "hunter2" {
		t.Fatalf("item = %s", body)
	}

	// Update the item.
	resp, _ = do(t, "PUT", base+"/data/secrets/db_password", `{"id":"db_password","value":"changed"}`)
	if resp.StatusCode != 200 {
		t.Fatalf("put item = %d", resp.StatusCode)
	}
	_, body = do(t, "GET", base+"/data/secrets/db_password", "")
	json.Unmarshal([]byte(body), &item)
	if item["value"] != "changed" {
		t.Fatalf("item not updated: %s", body)
	}

	// Item list now shows the item.
	_, body = do(t, "GET", base+"/data/secrets", "")
	var items map[string]string
	json.Unmarshal([]byte(body), &items)
	if _, ok := items["db_password"]; !ok {
		t.Fatalf("item list = %s", body)
	}

	// Delete the item.
	resp, _ = do(t, "DELETE", base+"/data/secrets/db_password", "")
	if resp.StatusCode != 200 {
		t.Fatalf("delete item = %d", resp.StatusCode)
	}
	resp, _ = do(t, "GET", base+"/data/secrets/db_password", "")
	if resp.StatusCode != 404 {
		t.Fatalf("get deleted item = %d", resp.StatusCode)
	}
}

func TestDataBagDeleteRemovesItems(t *testing.T) {
	srv, st := newTestAPI(t)
	base := srv.URL + "/organizations/acme"
	do(t, "POST", base+"/data", `{"name":"secrets"}`)
	do(t, "POST", base+"/data/secrets", `{"id":"a"}`)
	do(t, "POST", base+"/data/secrets", `{"id":"b"}`)

	resp, _ := do(t, "DELETE", base+"/data/secrets", "")
	if resp.StatusCode != 200 {
		t.Fatalf("delete bag = %d", resp.StatusCode)
	}
	// The bag and its items are gone.
	resp, _ = do(t, "GET", base+"/data/secrets", "")
	if resp.StatusCode != 404 {
		t.Fatalf("get deleted bag = %d", resp.StatusCode)
	}
	org, _, err := st.Org("acme")
	if err != nil {
		t.Fatal(err)
	}
	keys, err := org.Keys("databag_items:secrets")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Fatal("items not removed with bag")
	}
}

func TestDataBagItemRequiresID(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/organizations/acme"
	do(t, "POST", base+"/data", `{"name":"secrets"}`)
	resp, _ := do(t, "POST", base+"/data/secrets", `{"value":"no id"}`)
	if resp.StatusCode != 400 {
		t.Fatalf("item without id = %d, want 400", resp.StatusCode)
	}
}

func TestDataBagItemInMissingBag404(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/organizations/acme"
	resp, _ := do(t, "GET", base+"/data/ghost", "")
	if resp.StatusCode != 404 {
		t.Fatalf("missing bag = %d", resp.StatusCode)
	}
	resp, _ = do(t, "POST", base+"/data/ghost", `{"id":"x"}`)
	if resp.StatusCode != 404 {
		t.Fatalf("item in missing bag = %d", resp.StatusCode)
	}
}
