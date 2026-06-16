package state

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tas50/cinc-zero/internal/store"
)

// seedDir is the committed dev/test-repo state directory, relative to this
// package.
const seedDir = "../../dev/test-repo"

// loadSeed loads the committed dev/test-repo into a fresh store and returns the
// store, the acme org, and the load summary.
func loadSeed(t *testing.T) (*store.Store, *store.Org, *Summary) {
	t.Helper()
	st := store.New()
	sum, err := Load(st, seedDir)
	if err != nil {
		t.Fatalf("load %s: %v", seedDir, err)
	}
	org, ok := st.Org("acme")
	if !ok {
		t.Fatal("seed did not create org acme")
	}
	return st, org, sum
}

// TestSeedCounts pins the shape of the committed dev/test-repo: a medium,
// realistic fleet with global users and a group the chef-repo format alone
// cannot express.
func TestSeedCounts(t *testing.T) {
	_, _, sum := loadSeed(t)

	if sum.Users != 2 {
		t.Errorf("global users = %d, want 2", sum.Users)
	}
	acme := sum.Orgs["acme"]
	for coll, want := range map[string]int{
		"nodes":            24,
		"roles":            8,
		"environments":     3,
		"cookbooks":        8,
		"policy_groups":    2,
		"policy_revisions": 2,
	} {
		if got := acme.Counts[coll]; got != want {
			t.Errorf("acme %s = %d, want %d", coll, got, want)
		}
	}
	if acme.Groups != 1 {
		t.Errorf("acme groups = %d, want 1 (devs)", acme.Groups)
	}
}

// TestSeedFauxhaiNodes verifies the nodes carry real fauxhai automatic
// attributes: each node reports a platform and platform_version.
func TestSeedFauxhaiNodes(t *testing.T) {
	_, org, _ := loadSeed(t)

	platforms := map[string]bool{}
	for _, name := range org.Keys("nodes") {
		raw, _ := org.Get("nodes", name)
		var node struct {
			Automatic struct {
				Platform        string `json:"platform"`
				PlatformVersion string `json:"platform_version"`
			} `json:"automatic"`
		}
		if err := json.Unmarshal(raw, &node); err != nil {
			t.Fatalf("node %s: %v", name, err)
		}
		if node.Automatic.Platform == "" || node.Automatic.PlatformVersion == "" {
			t.Errorf("node %s missing fauxhai automatic platform/version", name)
		}
		platforms[node.Automatic.Platform] = true
	}
	// The fleet spans several platforms, not just one.
	if len(platforms) < 4 {
		t.Errorf("fleet spans %d platforms, want >= 4 for realism", len(platforms))
	}
}

// TestSeedEnvironmentsHaveAttributesAndPins verifies the seed's environments
// are realistic: each carries some default/override attributes and pins at
// least one cookbook version, so the fixture exercises environment overrides
// and version constraints rather than shipping empty stubs.
func TestSeedEnvironmentsHaveAttributesAndPins(t *testing.T) {
	_, org, _ := loadSeed(t)

	for _, name := range org.Keys("environments") {
		if name == "_default" {
			continue
		}
		raw, _ := org.Get("environments", name)
		var env struct {
			DefaultAttributes  map[string]json.RawMessage `json:"default_attributes"`
			OverrideAttributes map[string]json.RawMessage `json:"override_attributes"`
			CookbookVersions   map[string]string          `json:"cookbook_versions"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("environment %s: %v", name, err)
		}

		if len(env.DefaultAttributes) == 0 && len(env.OverrideAttributes) == 0 {
			t.Errorf("environment %s has no default/override attributes", name)
		}
		if len(env.CookbookVersions) == 0 {
			t.Errorf("environment %s pins no cookbook versions", name)
		}
	}
}

// TestSeedRunListCookbooksLoaded verifies every cookbook named by a role
// run-list recipe (recipe[<cookbook>::<recipe>]) is loaded into the seed, so
// the fixture can actually resolve the recipes its nodes converge.
func TestSeedRunListCookbooksLoaded(t *testing.T) {
	_, org, _ := loadSeed(t)

	loaded := map[string]bool{}
	for _, key := range org.Keys("cookbooks") {
		name, _, _ := strings.Cut(key, "/")
		loaded[name] = true
	}

	referenced := map[string]bool{}
	for _, name := range org.Keys("roles") {
		raw, _ := org.Get("roles", name)
		var role struct {
			RunList []string `json:"run_list"`
		}
		if err := json.Unmarshal(raw, &role); err != nil {
			t.Fatalf("role %s: %v", name, err)
		}
		for _, item := range role.RunList {
			if !strings.HasPrefix(item, "recipe[") {
				continue
			}
			spec := strings.TrimSuffix(strings.TrimPrefix(item, "recipe["), "]")
			cookbook, _, _ := strings.Cut(spec, "::")
			referenced[cookbook] = true
		}
	}

	if len(referenced) == 0 {
		t.Fatal("no recipe references found in role run-lists")
	}
	for cookbook := range referenced {
		if !loaded[cookbook] {
			t.Errorf("role run-lists reference cookbook %q but it is not loaded", cookbook)
		}
	}
}

// TestSeedNoDanglingReferences verifies every node points at objects that
// actually exist in the seed: run-list roles, chef_environment, and (for
// policy nodes) a policy revision pinned into the node's policy group.
func TestSeedNoDanglingReferences(t *testing.T) {
	_, org, _ := loadSeed(t)

	has := func(coll, key string) bool {
		_, ok := org.Get(coll, key)
		return ok
	}

	for _, name := range org.Keys("nodes") {
		raw, _ := org.Get("nodes", name)
		var node struct {
			ChefEnvironment string   `json:"chef_environment"`
			RunList         []string `json:"run_list"`
			PolicyName      string   `json:"policy_name"`
			PolicyGroup     string   `json:"policy_group"`
		}
		if err := json.Unmarshal(raw, &node); err != nil {
			t.Fatalf("node %s: %v", name, err)
		}

		if env := node.ChefEnvironment; env != "" && env != "_default" && !has("environments", env) {
			t.Errorf("node %s references missing environment %q", name, env)
		}

		for _, item := range node.RunList {
			role := strings.TrimSuffix(strings.TrimPrefix(item, "role["), "]")
			if strings.HasPrefix(item, "role[") && !has("roles", role) {
				t.Errorf("node %s run-list references missing role %q", name, role)
			}
		}

		if node.PolicyName != "" {
			if !has("policy_groups", node.PolicyGroup) {
				t.Errorf("node %s references missing policy_group %q", name, node.PolicyGroup)
				continue
			}
			groupRaw, _ := org.Get("policy_groups", node.PolicyGroup)
			var group struct {
				Policies map[string]struct {
					RevisionID string `json:"revision_id"`
				} `json:"policies"`
			}
			if err := json.Unmarshal(groupRaw, &group); err != nil {
				t.Fatalf("policy_group %s: %v", node.PolicyGroup, err)
			}
			pin, ok := group.Policies[node.PolicyName]
			if !ok {
				t.Errorf("node %s policy %q not pinned in group %q", name, node.PolicyName, node.PolicyGroup)
				continue
			}
			if !has("policy_revisions:"+node.PolicyName, pin.RevisionID) {
				t.Errorf("node %s policy %q revision %q not loaded", name, node.PolicyName, pin.RevisionID)
			}
		}
	}
}
