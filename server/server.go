// Package server assembles the in-memory store, the Chef API handlers, and the
// authentication layer into a runnable, embeddable Chef Infra Server. It is the
// public entry point for Go test suites:
//
//	srv, _ := server.New(server.Options{Orgs: []string{"test"}})
//	srv.Start()
//	defer srv.Stop(context.Background())
//	// talk to srv.URL() with a client signed by srv.AdminKey()
package server

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/tas50/cinc-zero/internal/api"
	"github.com/tas50/cinc-zero/internal/auth"
	"github.com/tas50/cinc-zero/internal/repo"
	"github.com/tas50/cinc-zero/internal/store"
)

// Options configures a Server. The zero value is usable: it creates a single
// organization named "acme", an admin user named "pivotal", and enforces real
// Mixlib authentication with a 15-minute clock-skew window.
type Options struct {
	// Addr is the listen address. Defaults to "127.0.0.1:0" (random port).
	Addr string
	// Orgs are the organizations to create at startup. Defaults to ["acme"].
	Orgs []string
	// AdminName is the bootstrap admin user. Defaults to "pivotal".
	AdminName string
	// DisableAuth skips signature verification entirely (useful for tests that
	// do not want to sign requests).
	DisableAuth bool
	// EnforceACL turns on authorization enforcement: object ACLs and group
	// membership actually gate requests, and unauthorized operations return
	// 403. Defaults to false (every authenticated actor is permitted), which
	// keeps existing test pipelines unaffected. Requires authentication, so it
	// cannot be combined with DisableAuth.
	EnforceACL bool
	// SkewSeconds is the allowed clock skew for request timestamps. Defaults to
	// 900 (15 minutes).
	SkewSeconds int
	// Now is the clock used for skew checks. Defaults to time.Now.
	Now func() time.Time
	// Repo is an optional path to a chef-repo whose objects (nodes, roles,
	// environments, clients, policies, policy_groups, data bags) are loaded
	// into the first organization at startup, mirroring `knife upload`.
	Repo string
}

func (o *Options) withDefaults() {
	if o.Addr == "" {
		o.Addr = "127.0.0.1:0"
	}
	if len(o.Orgs) == 0 {
		o.Orgs = []string{"acme"}
	}
	if o.AdminName == "" {
		o.AdminName = "pivotal"
	}
	if o.SkewSeconds == 0 {
		o.SkewSeconds = 900
	}
	if o.Now == nil {
		o.Now = time.Now
	}
}

// Server is a running (or ready-to-run) cinc-zero instance.
type Server struct {
	opts          Options
	store         *store.Store
	handler       http.Handler
	httpSrv       *http.Server
	listener      net.Listener
	adminKey      []byte            // PEM-encoded admin private key
	validatorKeys map[string][]byte // org name -> PEM-encoded validator private key
	url           string
}

// New builds a Server: it creates the store, bootstraps the admin user and the
// configured organizations, and wires the API behind the auth layer. It does
// not begin listening; call Start for that.
func New(opts Options) (*Server, error) {
	opts.withDefaults()
	if opts.DisableAuth && opts.EnforceACL {
		return nil, errors.New("EnforceACL requires authentication; do not set DisableAuth")
	}
	st := store.New()

	// Bootstrap the admin user with a fresh key pair.
	key, err := auth.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("generate admin key: %w", err)
	}
	pubPEM, err := auth.EncodePublicKeyPEM(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	adminDoc := fmt.Sprintf(`{"username":%q,"admin":true,"public_key":%q}`, opts.AdminName, string(pubPEM))
	st.Global().Put("users", opts.AdminName, []byte(adminDoc))

	validatorKeys := make(map[string][]byte, len(opts.Orgs))
	for _, name := range opts.Orgs {
		validator, err := api.CreateOrganization(st, name, name)
		if err != nil {
			return nil, fmt.Errorf("create org %q: %w", name, err)
		}
		validatorKeys[name] = validator
	}

	// Load a chef-repo into the first organization, if configured.
	if opts.Repo != "" {
		org, ok := st.Org(opts.Orgs[0])
		if !ok {
			return nil, fmt.Errorf("repo target org %q not found", opts.Orgs[0])
		}
		if _, err := repo.Load(org, opts.Repo); err != nil {
			return nil, fmt.Errorf("load repo %q: %w", opts.Repo, err)
		}
	}

	s := &Server{
		opts:          opts,
		store:         st,
		adminKey:      auth.EncodePrivateKeyPEM(key),
		validatorKeys: validatorKeys,
	}
	handler := api.New(st, api.WithACLEnforcement(opts.EnforceACL)).Handler()
	if !opts.DisableAuth {
		handler = s.authMiddleware(handler)
	}
	s.handler = handler
	return s, nil
}

// Start binds the listener and serves in a background goroutine.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.opts.Addr)
	if err != nil {
		return err
	}
	s.listener = ln
	s.url = "http://" + ln.Addr().String()
	s.httpSrv = &http.Server{Handler: s.handler}
	go func() {
		if err := s.httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			// Serve errors after Close are expected; nothing to do here.
			_ = err
		}
	}()
	return nil
}

// Stop gracefully shuts the server down.
func (s *Server) Stop(ctx context.Context) error {
	if s.httpSrv == nil {
		return nil
	}
	return s.httpSrv.Shutdown(ctx)
}

// URL is the base URL the server is listening on (valid after Start).
func (s *Server) URL() string { return s.url }

// AdminKey returns the PEM-encoded private key for the admin user. Sign
// requests with this key (and AdminName) to act as the administrator.
func (s *Server) AdminKey() []byte { return s.adminKey }

// AdminName returns the bootstrap admin user name.
func (s *Server) AdminName() string { return s.opts.AdminName }

// ValidatorKey returns the PEM-encoded private key for an organization's
// "<org>-validator" client, or nil if the org was not created at bootstrap.
// This is the key chef-client uses to register new nodes.
func (s *Server) ValidatorKey(org string) []byte { return s.validatorKeys[org] }

// Store exposes the underlying store for programmatic seeding and inspection.
func (s *Server) Store() *store.Store { return s.store }

// publicKeyFor resolves an actor's RSA public key, checking org clients (when
// the request targets an org) and then global users.
func (s *Server) publicKeyFor(path, actor string) (*rsa.PublicKey, bool) {
	if org := orgFromPath(path); org != "" {
		if o, ok := s.store.Org(org); ok {
			if pub, ok := actorKey(o, "clients", actor); ok {
				return pub, true
			}
		}
	}
	if pub, ok := actorKey(s.store.Global(), "users", actor); ok {
		return pub, true
	}
	return nil, false
}
