package main

import (
	"bytes"
	"strings"
	"testing"
)

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
