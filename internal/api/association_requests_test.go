package api

import (
	"encoding/json"
	"testing"
)

// inviteID creates a global user, invites them to acme, and returns the
// invitation id.
func invite(t *testing.T, srvURL, user string) string {
	t.Helper()
	do(t, "POST", srvURL+"/users", `{"name":"`+user+`"}`)
	resp, body := do(t, "POST", srvURL+"/organizations/acme/association_requests", `{"user":"`+user+`"}`)
	if resp.StatusCode != 201 {
		t.Fatalf("invite %s = %d: %s", user, resp.StatusCode, body)
	}
	var got struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(body), &got)
	if got.ID == "" {
		t.Fatalf("invite response missing id: %s", body)
	}
	return got.ID
}

func TestAssociationRequestAcceptFlow(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/organizations/acme"
	id := invite(t, srv.URL, "alice")

	// Duplicate invite is rejected.
	resp, _ := do(t, "POST", base+"/association_requests", `{"user":"alice"}`)
	if resp.StatusCode != 409 {
		t.Fatalf("duplicate invite = %d, want 409", resp.StatusCode)
	}

	// Inviting an unknown user fails.
	resp, _ = do(t, "POST", base+"/association_requests", `{"user":"ghost"}`)
	if resp.StatusCode != 404 {
		t.Fatalf("invite unknown user = %d, want 404", resp.StatusCode)
	}

	// The invite shows up on the org side.
	_, body := do(t, "GET", base+"/association_requests", "")
	var orgInvites []struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	json.Unmarshal([]byte(body), &orgInvites)
	if len(orgInvites) != 1 || orgInvites[0].Username != "alice" {
		t.Fatalf("org invites = %s", body)
	}

	// And on the user side, with a count.
	_, body = do(t, "GET", srv.URL+"/users/alice/association_requests", "")
	var userInvites []struct {
		ID      string `json:"id"`
		OrgName string `json:"orgname"`
	}
	json.Unmarshal([]byte(body), &userInvites)
	if len(userInvites) != 1 || userInvites[0].OrgName != "acme" {
		t.Fatalf("user invites = %s", body)
	}
	_, body = do(t, "GET", srv.URL+"/users/alice/association_requests/count", "")
	var count struct {
		Value int `json:"value"`
	}
	json.Unmarshal([]byte(body), &count)
	if count.Value != 1 {
		t.Fatalf("invite count = %s", body)
	}

	// Accept the invite: alice becomes an org member and the invite clears.
	resp, body = do(t, "PUT", srv.URL+"/users/alice/association_requests/"+id, `{"response":"accept"}`)
	if resp.StatusCode != 200 {
		t.Fatalf("accept = %d: %s", resp.StatusCode, body)
	}
	resp, _ = do(t, "GET", base+"/users/alice", "")
	if resp.StatusCode != 200 {
		t.Fatalf("alice should be an org member after accept, got %d", resp.StatusCode)
	}
	_, body = do(t, "GET", srv.URL+"/users/alice/association_requests/count", "")
	json.Unmarshal([]byte(body), &count)
	if count.Value != 0 {
		t.Fatalf("invite count after accept = %s", body)
	}
}

func TestAssociationRequestRejectFlow(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/organizations/acme"
	id := invite(t, srv.URL, "bob")

	resp, body := do(t, "PUT", srv.URL+"/users/bob/association_requests/"+id, `{"response":"reject"}`)
	if resp.StatusCode != 200 {
		t.Fatalf("reject = %d: %s", resp.StatusCode, body)
	}
	// bob is not a member, and the invite is gone.
	resp, _ = do(t, "GET", base+"/users/bob", "")
	if resp.StatusCode != 404 {
		t.Fatalf("bob should not be a member after reject, got %d", resp.StatusCode)
	}
}

func TestAssociationRequestRescind(t *testing.T) {
	srv, _ := newTestAPI(t)
	base := srv.URL + "/organizations/acme"
	id := invite(t, srv.URL, "carol")

	resp, _ := do(t, "DELETE", base+"/association_requests/"+id, "")
	if resp.StatusCode != 200 {
		t.Fatalf("rescind = %d, want 200", resp.StatusCode)
	}
	resp, _ = do(t, "DELETE", base+"/association_requests/"+id, "")
	if resp.StatusCode != 404 {
		t.Fatalf("rescind again = %d, want 404", resp.StatusCode)
	}
}
