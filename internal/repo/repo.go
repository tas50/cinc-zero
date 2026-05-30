// Package repo loads an on-disk chef-repo into an organization's store,
// mirroring the directory shapes that `knife upload` expects. JSON object types
// (nodes, roles, environments, clients, policies, policy_groups) and data bags
// are supported. Loading cookbook directories — which requires synthesizing a
// manifest and checksumming files — is a separate, future increment.
package repo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tas50/cinc-zero/internal/store"
)

// Summary reports how many objects of each kind were loaded.
type Summary struct {
	Counts map[string]int
}

// These collection names mirror the storage layout used by the API handlers.
const (
	dataBagsColl = "data_bags"
)

func dataBagItemsColl(bag string) string { return "databag_items:" + bag }

// objectDir maps a chef-repo directory to its store collection and the field
// that names each object.
type objectDir struct {
	dir, collection, nameField string
}

var objectDirs = []objectDir{
	{"nodes", "nodes", "name"},
	{"roles", "roles", "name"},
	{"environments", "environments", "name"},
	{"clients", "clients", "name"},
	{"policies", "policies", "name"},
	{"policy_groups", "policy_groups", "name"},
}

// Load walks the chef-repo at root, inserting its objects into org. A missing
// root (or missing subdirectory) is not an error — it simply loads nothing.
func Load(org *store.Org, root string) (*Summary, error) {
	sum := &Summary{Counts: map[string]int{}}

	for _, od := range objectDirs {
		n, err := loadObjects(org, filepath.Join(root, od.dir), od.collection, od.nameField)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			sum.Counts[od.collection] += n
		}
	}

	bags, items, err := loadDataBags(org, filepath.Join(root, dataBagsColl))
	if err != nil {
		return nil, err
	}
	if bags > 0 {
		sum.Counts["data_bags"] = bags
		sum.Counts["data_bag_items"] = items
	}

	cookbooks, err := loadCookbooks(org, filepath.Join(root, "cookbooks"))
	if err != nil {
		return nil, err
	}
	if cookbooks > 0 {
		sum.Counts["cookbooks"] = cookbooks
	}
	return sum, nil
}

// loadObjects stores every *.json file in dir under collection, keyed by the
// object's nameField (falling back to the filename).
func loadObjects(org *store.Org, dir, collection, nameField string) (int, error) {
	entries, err := jsonFiles(dir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, path := range entries {
		obj, raw, err := readObject(path)
		if err != nil {
			return 0, err
		}
		org.Put(collection, objectKey(obj, nameField, path), raw)
		count++
	}
	return count, nil
}

// loadDataBags registers each subdirectory of dir as a data bag and loads its
// items (keyed by "id"). Returns the bag and item counts.
func loadDataBags(org *store.Org, dir string) (bags, items int, err error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		bag := e.Name()
		org.Put(dataBagsColl, bag, fmt.Appendf(nil, `{"name":%q}`, bag))
		bags++

		files, err := jsonFiles(filepath.Join(dir, bag))
		if err != nil {
			return 0, 0, err
		}
		for _, path := range files {
			obj, raw, err := readObject(path)
			if err != nil {
				return 0, 0, err
			}
			org.Put(dataBagItemsColl(bag), objectKey(obj, "id", path), raw)
			items++
		}
	}
	return bags, items, nil
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
	return out, nil
}

// readObject reads and canonicalizes a JSON object file.
func readObject(path string) (map[string]any, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	return obj, canonicalize(obj), nil
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

// canonicalize re-encodes obj as compact JSON without HTML escaping, matching
// how the API handlers persist objects.
func canonicalize(obj map[string]any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(obj)
	return bytes.TrimRight(buf.Bytes(), "\n")
}
