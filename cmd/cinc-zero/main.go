// Command cinc-zero runs the in-memory Chef Infra Server as a standalone
// process for use in test pipelines.
package main

import (
	"context"
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

func run(args []string, out io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "version", "--version", "-version":
			fmt.Fprintf(out, "cinc-zero %s (commit %s, built %s)\n", version, commit, buildDate)
			return nil
		}
	}

	fs := flag.NewFlagSet("cinc-zero", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8889", "listen address (host:port)")
	orgsCSV := fs.String("orgs", "acme", "comma-separated organizations to create")
	admin := fs.String("admin", "pivotal", "bootstrap admin user name")
	keyFile := fs.String("key-out", "", "write the admin private key to this file")
	noAuth := fs.Bool("no-auth", false, "disable request signature verification")
	repoPath := fs.String("repo", "", "path to a chef-repo to load into the first org at startup")
	if err := fs.Parse(args); err != nil {
		return err
	}

	srv, err := server.New(server.Options{
		Addr:        *addr,
		Orgs:        splitCSV(*orgsCSV),
		AdminName:   *admin,
		DisableAuth: *noAuth,
		Repo:        *repoPath,
	})
	if err != nil {
		return err
	}
	if err := srv.Start(); err != nil {
		return err
	}

	if *keyFile != "" {
		if err := os.WriteFile(*keyFile, srv.AdminKey(), 0o600); err != nil {
			return fmt.Errorf("write key file: %w", err)
		}
		fmt.Fprintf(out, "Admin key for %q written to %s\n", srv.AdminName(), *keyFile)
	}
	fmt.Fprintf(out, "cinc-zero listening on %s\n", srv.URL())
	fmt.Fprintf(out, "  orgs: %s\n  admin user: %s (auth %s)\n",
		*orgsCSV, srv.AdminName(), authState(*noAuth))
	if *repoPath != "" {
		fmt.Fprintf(out, "  loaded chef-repo from %s\n", *repoPath)
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
