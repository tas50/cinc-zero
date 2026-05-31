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
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
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
	keyCache      *auth.PublicKeyCache
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

	// RSA-2048 key generation is the dominant startup cost and each key is
	// independent, so generate the admin key and every org's validator key in
	// parallel, then seed the store serially with the results. keys[0] is the
	// admin key; keys[i+1] is the validator key for opts.Orgs[i].
	keys, err := generateKeys(len(opts.Orgs) + 1)
	if err != nil {
		return nil, err
	}

	// Bootstrap the admin user.
	pubPEM, err := auth.EncodePublicKeyPEM(&keys[0].PublicKey)
	if err != nil {
		return nil, err
	}
	adminDoc := fmt.Sprintf(`{"username":%q,"admin":true,"public_key":%q}`, opts.AdminName, string(pubPEM))
	st.Global().Put("users", opts.AdminName, []byte(adminDoc))

	validatorKeys := make(map[string][]byte, len(opts.Orgs))
	for i, name := range opts.Orgs {
		validator, err := api.CreateOrganizationWithKey(st, name, name, keys[i+1])
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
		adminKey:      auth.EncodePrivateKeyPEM(keys[0]),
		validatorKeys: validatorKeys,
		keyCache:      auth.NewPublicKeyCache(),
	}
	handler := api.New(st, api.WithACLEnforcement(opts.EnforceACL)).Handler()
	if !opts.DisableAuth {
		handler = s.authMiddleware(handler)
	}
	s.handler = handler
	return s, nil
}

// generateKeys generates n RSA key pairs concurrently and returns them in order.
// Key generation is CPU-bound and independent, so running it in parallel cuts
// startup well below the serial (n × keygen) cost. Each goroutine reads entropy
// through its own buffered reader so they do not serialize on crypto/rand's
// global lock during prime search.
func generateKeys(n int) ([]*rsa.PrivateKey, error) {
	keys := make([]*rsa.PrivateKey, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range keys {
		go func(i int) {
			defer wg.Done()
			keys[i], errs[i] = auth.GenerateKeyFrom(bufio.NewReaderSize(rand.Reader, 1<<16))
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("generate key: %w", err)
		}
	}
	return keys, nil
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

	// Warm the serving path so the first real client request is not the one that
	// pays for cold connection handling, code paths, pools, and GC heap growth.
	s.prewarm()
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
