package main

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"strings"
	"testing"
)

// TestStateFlagHiddenFromUsage verifies the experimental --state flag is parsed
// but kept out of the usage/help output, while a documented flag (--repo) still
// appears.
func TestStateFlagHiddenFromUsage(t *testing.T) {
	var buf bytes.Buffer
	_, err := parseFlags([]string{"-h"}, &buf)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("parseFlags(-h) error = %v, want flag.ErrHelp", err)
	}
	out := buf.String()
	if strings.Contains(out, "-state") {
		t.Errorf("usage should hide -state, got:\n%s", out)
	}
	if !strings.Contains(out, "-repo") {
		t.Errorf("usage should show -repo, got:\n%s", out)
	}
}

// TestStateFlagSetsStatePath verifies --state populates the parsed flags.
func TestStateFlagSetsStatePath(t *testing.T) {
	f, err := parseFlags([]string{"--state", "/tmp/seed"}, io.Discard)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if f.state != "/tmp/seed" {
		t.Errorf("state = %q, want /tmp/seed", f.state)
	}
}

// TestVersionSubcommand verifies the "version" subcommand prints the injected
// build metadata and exits without starting the server.
func TestVersionSubcommand(t *testing.T) {
	version, commit, buildDate = "v1.2.3", "abc1234", "2026-05-30"

	for _, args := range [][]string{{"version"}, {"--version"}, {"-version"}} {
		var buf bytes.Buffer
		if err := run(args, &buf); err != nil {
			t.Fatalf("run(%v) returned error: %v", args, err)
		}
		out := buf.String()
		for _, want := range []string{"cinc-zero", "v1.2.3", "abc1234", "2026-05-30"} {
			if !strings.Contains(out, want) {
				t.Errorf("run(%v) output %q missing %q", args, out, want)
			}
		}
	}
}
