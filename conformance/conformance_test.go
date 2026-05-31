//go:build conformance

// Package conformance drives the real knife CLI (from Cinc Workstation) against
// an in-process cinc-zero server, exercising the full signed-request lifecycle:
// reads, writes, search, and the cookbook sandbox/upload flow. It is gated
// behind the "conformance" build tag and a runnable knife binary, so it only
// executes where knife is installed (e.g. CI after the Cinc Workstation
// omnitruck install). Run with: go test -tags conformance ./conformance/
package conformance

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tas50/cinc-zero/server"
)

// knifeBin locates a runnable knife. Honor $KNIFE, else look on PATH. If knife
// cannot even report its version (e.g. a broken local Ruby), the suite skips.
func knifeBin(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("KNIFE")
	if bin == "" {
		var err error
		if bin, err = exec.LookPath("knife"); err != nil {
			t.Skip("knife not found on PATH; set $KNIFE or install cinc-workstation")
		}
	}
	if out, err := exec.Command(bin, "--version").CombinedOutput(); err != nil {
		t.Skipf("knife (%s) is not runnable, skipping conformance: %v\n%s", bin, err, out)
	}
	return bin
}

// harness starts a cinc-zero server with auth enabled and writes a knife.rb +
// admin key + a sample cookbook into a temp workspace.
type harness struct {
	knife   string
	dir     string
	knifeRB string
}

func setup(t *testing.T) *harness {
	t.Helper()
	knife := knifeBin(t)

	srv, err := server.New(server.Options{Orgs: []string{"acme"}})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("server.Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		srv.Stop(ctx)
	})

	dir := t.TempDir()
	write(t, filepath.Join(dir, "admin.pem"), string(srv.AdminKey()))
	write(t, filepath.Join(dir, "cookbooks", "mycook", "metadata.rb"), "name 'mycook'\nversion '0.1.0'\n")
	write(t, filepath.Join(dir, "cookbooks", "mycook", "recipes", "default.rb"), "package 'nginx'\n")

	knifeRB := filepath.Join(dir, "knife.rb")
	write(t, knifeRB, strings.Join([]string{
		"node_name 'pivotal'",
		"client_key '" + filepath.Join(dir, "admin.pem") + "'",
		"chef_server_url '" + srv.URL() + "/organizations/acme'",
		"ssl_verify_mode :verify_none",
		"cookbook_path ['" + filepath.Join(dir, "cookbooks") + "']",
		"",
	}, "\n"))

	return &harness{knife: knife, dir: dir, knifeRB: knifeRB}
}

// run executes a knife subcommand and fails the test on a non-zero exit.
func (h *harness) run(t *testing.T, args ...string) string {
	t.Helper()
	args = append(args, "--config", h.knifeRB)
	cmd := exec.Command(h.knife, args...)
	cmd.Env = append(os.Environ(), "HOME="+h.dir) // avoid the user's ~/.chef
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("knife %s\n  error: %v\n  output: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestKnifeConformance(t *testing.T) {
	h := setup(t)

	// Reads: the seeded _default environment is visible over a signed request.
	if out := h.run(t, "environment", "list"); !strings.Contains(out, "_default") {
		t.Fatalf("environment list missing _default: %s", out)
	}

	// Node write + read-back + search.
	write(t, filepath.Join(h.dir, "web01.json"),
		`{"name":"web01","chef_environment":"_default","json_class":"Chef::Node","normal":{"role":"frontend"}}`)
	h.run(t, "node", "from", "file", filepath.Join(h.dir, "web01.json"))
	if out := h.run(t, "node", "list"); !strings.Contains(out, "web01") {
		t.Fatalf("node list missing web01: %s", out)
	}
	if out := h.run(t, "node", "show", "web01"); !strings.Contains(out, "web01") {
		t.Fatalf("node show missing web01: %s", out)
	}
	if out := h.run(t, "search", "node", "role:frontend"); !strings.Contains(out, "web01") {
		t.Fatalf("search did not find web01: %s", out)
	}

	// Data bag create + item upload + read-back.
	h.run(t, "data", "bag", "create", "testbag")
	if out := h.run(t, "data", "bag", "list"); !strings.Contains(out, "testbag") {
		t.Fatalf("data bag list missing testbag: %s", out)
	}
	write(t, filepath.Join(h.dir, "item1.json"), `{"id":"item1","secret":"value"}`)
	h.run(t, "data", "bag", "from", "file", "testbag", filepath.Join(h.dir, "item1.json"))
	if out := h.run(t, "data", "bag", "show", "testbag", "item1"); !strings.Contains(out, "value") {
		t.Fatalf("data bag item read-back failed: %s", out)
	}

	// Cookbook upload: sandbox -> file store -> commit -> cookbook PUT.
	h.run(t, "cookbook", "upload", "mycook")
	if out := h.run(t, "cookbook", "list"); !strings.Contains(out, "mycook") {
		t.Fatalf("cookbook list missing mycook: %s", out)
	}
}
