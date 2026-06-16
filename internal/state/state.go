// Package state loads a complete cinc-zero server's state from an on-disk
// directory: global users, every organization, and each org's chef-objects
// plus authz groups. It is a superset of internal/repo, which loads only a
// single org's chef-objects; state reuses repo.Load for those and adds the
// pieces the chef-repo format cannot express (global users, multiple orgs,
// groups).
//
// Layout:
//
//	<root>/
//	  users/<name>.json                       global users
//	  organizations/<org>/                     one dir per org
//	    nodes/ roles/ environments/ clients/    loaded via repo.Load
//	    policies/ policy_groups/ data_bags/ cookbooks/
//	    groups/<group>.json                     authz groups
//
// A missing users/, organizations/, or any per-org subdirectory is not an
// error — it simply loads nothing.
package state

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tas50/cinc-zero/internal/api"
	"github.com/tas50/cinc-zero/internal/repo"
	"github.com/tas50/cinc-zero/internal/store"
)

// Summary reports what Load hydrated.
type Summary struct {
	// Users is the number of global users loaded.
	Users int
	// Orgs maps each organization name to its load summary.
	Orgs map[string]OrgSummary
}

// OrgSummary reports what was loaded into a single organization.
type OrgSummary struct {
	// Counts maps collection name to the number of objects loaded (from the
	// underlying repo loader).
	Counts map[string]int
	// Groups is the number of authz groups loaded.
	Groups int
}

// Load hydrates the entire server from the state directory at root.
func Load(st *store.Store, root string) (*Summary, error) {
	sum := &Summary{Orgs: map[string]OrgSummary{}}

	users, err := loadGlobalUsers(st, filepath.Join(root, "users"))
	if err != nil {
		return nil, err
	}
	sum.Users = users

	orgsRoot := filepath.Join(root, "organizations")
	orgNames, err := subdirs(orgsRoot)
	if err != nil {
		return nil, err
	}
	for _, name := range orgNames {
		org, err := orgOrCreate(st, name)
		if err != nil {
			return nil, err
		}
		orgDir := filepath.Join(orgsRoot, name)

		repoSum, err := repo.Load(org, orgDir)
		if err != nil {
			return nil, fmt.Errorf("org %q: %w", name, err)
		}
		groups, err := loadGroups(org, filepath.Join(orgDir, "groups"))
		if err != nil {
			return nil, fmt.Errorf("org %q: %w", name, err)
		}
		sum.Orgs[name] = OrgSummary{Counts: repoSum.Counts, Groups: groups}
	}
	return sum, nil
}

// orgOrCreate returns the existing org, or creates it (with its _default
// environment, validator client, and default groups/ACLs) if absent.
func orgOrCreate(st *store.Store, name string) (*store.Org, error) {
	if org, ok := st.Org(name); ok {
		return org, nil
	}
	if _, err := api.CreateOrganization(st, name, name); err != nil {
		return nil, fmt.Errorf("create org %q: %w", name, err)
	}
	org, _ := st.Org(name)
	return org, nil
}

// loadGlobalUsers stores every *.json file in dir as a global user, keyed by
// its "username" field (falling back to the filename).
func loadGlobalUsers(st *store.Store, dir string) (int, error) {
	files, err := jsonFiles(dir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, path := range files {
		obj, raw, err := readObject(path)
		if err != nil {
			return 0, err
		}
		st.Global().Put("users", objectKey(obj, "username", path), raw)
		count++
	}
	return count, nil
}

// loadGroups stores every *.json file in dir as an authz group in the org's
// "groups" collection, keyed by its "groupname" field (falling back to the
// filename).
func loadGroups(org *store.Org, dir string) (int, error) {
	files, err := jsonFiles(dir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, path := range files {
		obj, raw, err := readObject(path)
		if err != nil {
			return 0, err
		}
		org.Put("groups", objectKey(obj, "groupname", path), raw)
		count++
	}
	return count, nil
}

// subdirs returns the names of the immediate subdirectories of dir, sorted for
// deterministic load order. A missing dir yields no names and no error.
func subdirs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// jsonFiles returns the *.json files in dir, or nil if dir does not exist.
func jsonFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

// readObject reads a JSON object file and returns it both decoded and
// canonicalized (compact, no HTML escaping) to match how the API handlers
// persist objects.
func readObject(path string) (map[string]any, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(obj)
	return obj, bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// objectKey returns the value of nameField, or the filename without its
// extension when that field is absent.
func objectKey(obj map[string]any, nameField, path string) string {
	if name, ok := obj[nameField].(string); ok && name != "" {
		return name
	}
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
