package main

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func gen(t *testing.T) genResult {
	t.Helper()
	base := filepath.Join("..", "..", "dev", "test-repo", "organizations", "acme", "nodes")
	linux, windows, err := harvestTemplates(base)
	if err != nil {
		t.Fatalf("harvest: %v", err)
	}
	if len(linux) == 0 || len(windows) == 0 {
		t.Fatalf("templates: linux=%d windows=%d", len(linux), len(windows))
	}
	return generate(linux, windows, rand.New(rand.NewSource(1)))
}

func TestGenerateCounts(t *testing.T) {
	g := gen(t)
	total := 0
	for _, st := range serverTypes {
		total += st.count
	}
	if total != 1000 {
		t.Fatalf("serverTypes counts sum to %d, want 1000", total)
	}
	if len(g.nodes) != 1000 {
		t.Errorf("nodes = %d, want 1000", len(g.nodes))
	}
	if len(g.users) != 25 {
		t.Errorf("users = %d, want 25", len(g.users))
	}
	if len(g.apps) != 15 {
		t.Errorf("apps = %d, want 15", len(g.apps))
	}
	if len(g.secrets) != 10 {
		t.Errorf("secrets = %d, want 10", len(g.secrets))
	}
	if len(g.roles) != 13 {
		t.Errorf("roles = %d, want 13", len(g.roles))
	}
	if len(g.cookbooks) != 13 {
		t.Errorf("cookbooks = %d, want 13", len(g.cookbooks))
	}
	if len(g.envs) != 2 {
		t.Errorf("environments = %d, want 2 (qa, dr)", len(g.envs))
	}
}

// Each server type produces the right count of datacenter-coded, incrementing
// hostnames (e.g. web-iad-001).
func TestGenerateNamingPattern(t *testing.T) {
	g := gen(t)
	perPrefix := map[string]int{}
	for name := range g.nodes {
		prefix := name[:strings.IndexByte(name, '-')]
		perPrefix[prefix]++
	}
	for _, st := range serverTypes {
		if perPrefix[st.prefix] != st.count {
			t.Errorf("%s nodes = %d, want %d", st.prefix, perPrefix[st.prefix], st.count)
		}
		// The first node of each type lands in the first datacenter.
		first := fmt.Sprintf("%s-%s-001", st.prefix, dataCenters[0])
		if _, ok := g.nodes[first]; !ok {
			t.Errorf("missing expected node %q", first)
		}
	}
}

// No node references a role or environment that wouldn't exist in the baked DB,
// and every generated role installs a generated cookbook.
func TestGenerateNoDanglingReferences(t *testing.T) {
	g := gen(t)
	knownRoles := map[string]bool{ // base seed roles
		"base": true, "web": true, "database": true, "cache": true,
		"loadbalancer": true, "monitoring": true, "app": true, "ci": true,
	}
	for name := range g.roles { // plus everything seedgen generates
		knownRoles[name] = true
	}
	knownEnvs := map[string]bool{
		"production": true, "staging": true, "development": true, "qa": true, "dr": true,
	}

	for name, node := range g.nodes {
		if env, _ := node["chef_environment"].(string); !knownEnvs[env] {
			t.Errorf("node %s has unknown environment %q", name, env)
		}
		for _, item := range node["run_list"].([]any) {
			s := item.(string)
			if role, ok := strings.CutPrefix(s, "role["); ok {
				role = strings.TrimSuffix(role, "]")
				if !knownRoles[role] {
					t.Errorf("node %s references unknown role %q", name, role)
				}
			}
		}
	}

	// Every generated role installs a cookbook seedgen also generates.
	for name, role := range g.roles {
		for _, item := range role["run_list"].([]any) {
			s := item.(string)
			cb, ok := strings.CutPrefix(s, "recipe[")
			if !ok {
				continue
			}
			cb = strings.TrimSuffix(cb, "]")
			if cb == "chef-client" {
				continue // base seed cookbook
			}
			if _, ok := g.cookbooks[cb]; !ok {
				t.Errorf("role %s installs cookbook %q that seedgen does not generate", name, cb)
			}
		}
	}
	if _, ok := g.roles["domain_controller"]; !ok {
		t.Error("domain_controller role not generated")
	}
}

// Each node carries stamped identity matching its name (not the template's).
func TestGenerateStampsIdentity(t *testing.T) {
	g := gen(t)
	node := g.nodes["web-iad-001"]
	if node == nil {
		t.Fatal("expected node web-iad-001")
	}
	a := node["automatic"].(map[string]any)
	if a["hostname"] != "web-iad-001" {
		t.Errorf("hostname = %v, want web-iad-001", a["hostname"])
	}
	if a["fqdn"] != "web-iad-001.acme.example.com" {
		t.Errorf("fqdn = %v", a["fqdn"])
	}
	if _, ok := a["ipaddress"].(string); !ok {
		t.Error("ipaddress not stamped")
	}
}

// Generation is fully deterministic: same seed → identical output (so the baked
// DB does not churn on re-generation).
func TestGenerateIsDeterministic(t *testing.T) {
	base := filepath.Join("..", "..", "dev", "test-repo", "organizations", "acme", "nodes")
	linux, windows, err := harvestTemplates(base)
	if err != nil {
		t.Fatal(err)
	}
	a := generate(linux, windows, rand.New(rand.NewSource(1)))
	b := generate(linux, windows, rand.New(rand.NewSource(1)))
	if !reflect.DeepEqual(a.nodes, b.nodes) {
		t.Error("nodes differ between identical runs")
	}
	if !reflect.DeepEqual(a.secrets, b.secrets) {
		t.Error("secrets differ between identical runs")
	}
	if !reflect.DeepEqual(a.apps, b.apps) {
		t.Error("apps differ between identical runs")
	}
}
