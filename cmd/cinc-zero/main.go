// Command cinc-zero runs the in-memory Chef Infra Server as a standalone
// process for use in test pipelines.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/tas50/cinc-zero/server"
)

// Build metadata, injected at link time via -ldflags (see the Makefile).
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// cliFlags holds the parsed command-line options.
type cliFlags struct {
	addr        string
	orgsCSV     string
	admin       string
	keyFile     string
	noAuth      bool
	enforceACLs bool
	repo        string
	state       string
	webuiKey    string
}

// hiddenFlags are registered and functional but omitted from usage/help output
// (experimental knobs). --state is hidden while its on-disk format settles.
var hiddenFlags = map[string]bool{"state": true}

// parseFlags defines the cinc-zero flag set, writing usage to out, and parses
// args into cliFlags. The usage printer suppresses any flag in hiddenFlags.
func parseFlags(args []string, out io.Writer) (*cliFlags, error) {
	fs := flag.NewFlagSet("cinc-zero", flag.ContinueOnError)
	fs.SetOutput(out)

	var f cliFlags
	fs.StringVar(&f.addr, "addr", "127.0.0.1:8889", "listen address (host:port)")
	fs.StringVar(&f.orgsCSV, "orgs", "acme", "comma-separated organizations to create")
	fs.StringVar(&f.admin, "admin", "pivotal", "bootstrap admin user name")
	fs.StringVar(&f.keyFile, "key-out", "", "write the admin private key to this file")
	fs.BoolVar(&f.noAuth, "no-auth", false, "disable request signature verification")
	fs.BoolVar(&f.enforceACLs, "enforce-acls", false, "enforce object ACLs and group membership (default: permissive)")
	fs.StringVar(&f.repo, "repo", "", "path to a chef-repo to load into the first org at startup")
	fs.StringVar(&f.state, "state", "", "path to a full server-state directory to load at startup")
	fs.StringVar(&f.webuiKey, "webui-key", "", "path to a webui public/private key for X-Ops-Request-Source: web impersonation (defaults to the admin key)")

	fs.Usage = func() {
		fmt.Fprintf(out, "Usage of cinc-zero:\n")
		fs.VisitAll(func(fl *flag.Flag) {
			if hiddenFlags[fl.Name] {
				return
			}
			line := fmt.Sprintf("  -%s", fl.Name)
			if fl.DefValue != "" {
				line += fmt.Sprintf(" (default %q)", fl.DefValue)
			}
			fmt.Fprintf(out, "%s\n    \t%s\n", line, fl.Usage)
		})
	}

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return &f, nil
}

func run(args []string, out io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "version", "--version", "-version":
			fmt.Fprintf(out, "cinc-zero %s (commit %s, built %s)\n", version, commit, buildDate)
			return nil
		}
	}

	f, err := parseFlags(args, out)
	if err != nil {
		// -h/-help is a clean exit, not a failure.
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	var webuiKey []byte
	if f.webuiKey != "" {
		webuiKey, err = os.ReadFile(f.webuiKey)
		if err != nil {
			return fmt.Errorf("read webui key: %w", err)
		}
	}

	srv, err := server.New(server.Options{
		Addr:        f.addr,
		Orgs:        splitCSV(f.orgsCSV),
		AdminName:   f.admin,
		DisableAuth: f.noAuth,
		EnforceACL:  f.enforceACLs,
		Repo:        f.repo,
		StatePath:   f.state,
		WebUIKey:    webuiKey,
	})
	if err != nil {
		return err
	}
	if err := srv.Start(); err != nil {
		return err
	}

	if f.keyFile != "" {
		if err := os.WriteFile(f.keyFile, srv.AdminKey(), 0o600); err != nil {
			return fmt.Errorf("write key file: %w", err)
		}
		fmt.Fprintf(out, "Admin key for %q written to %s\n", srv.AdminName(), f.keyFile)
	}
	fmt.Fprintf(out, "cinc-zero listening on %s\n", srv.URL())
	fmt.Fprintf(out, "  orgs: %s\n  admin user: %s (auth %s, acl-enforcement %s)\n",
		f.orgsCSV, srv.AdminName(), authState(f.noAuth), enforceState(f.enforceACLs))
	if f.repo != "" {
		fmt.Fprintf(out, "  loaded chef-repo from %s\n", f.repo)
	}
	if f.state != "" {
		fmt.Fprintf(out, "  loaded server state from %s\n", f.state)
	}
	if !f.noAuth {
		webuiSrc := "admin key"
		if f.webuiKey != "" {
			webuiSrc = f.webuiKey
		}
		fmt.Fprintf(out, "  webui impersonation: enabled (key: %s)\n", webuiSrc)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	fmt.Fprintln(out, "shutting down...")
	return srv.Stop(context.Background())
}

func splitCSV(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func authState(disabled bool) string {
	if disabled {
		return "disabled"
	}
	return "enabled"
}

func enforceState(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "permissive"
}
