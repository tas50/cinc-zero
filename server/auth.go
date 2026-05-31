package server

import (
	"bytes"
	"crypto/rsa"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/tas50/cinc-zero/internal/api"
	"github.com/tas50/cinc-zero/internal/auth"
	"github.com/tas50/cinc-zero/internal/store"
)

// authMiddleware verifies the Mixlib signed-header authentication on every
// request (except unauthenticated system paths) before delegating to next.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// System paths (e.g. /_status) and the cookbook file store are
		// unauthenticated: in real Chef the sandbox hands back pre-signed
		// bookshelf URLs that knife/chef-client/cinc-client PUT/GET without
		// Mixlib signing, so the file store must accept those requests directly.
		if api.SystemPaths[r.URL.Path] || isFileStorePath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			unauthorized(w, "could not read request body")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))

		parsed, err := auth.Parse(r.Header)
		if err != nil {
			unauthorized(w, err.Error())
			return
		}

		pub, ok := s.publicKeyFor(r.URL.Path, parsed.UserID)
		if !ok {
			unauthorized(w, "Failed to authenticate as '"+parsed.UserID+"'. Ensure that your node_name and client key are correct.")
			return
		}

		if err := auth.Verify(r.Method, r.URL.Path, body, parsed, r.Header.Get("X-Ops-Server-API-Version"), pub); err != nil {
			unauthorized(w, "Invalid signature for user or client '"+parsed.UserID+"'")
			return
		}

		if err := s.checkSkew(parsed.Timestamp); err != nil {
			unauthorized(w, err.Error())
			return
		}

		// Carry the verified actor for the authorization layer (a no-op unless
		// EnforceACL is set).
		r = r.WithContext(api.WithActor(r.Context(), s.resolveActor(r.URL.Path, parsed.UserID)))
		next.ServeHTTP(w, r)
	})
}

// resolveActor builds the actor identity for authorization. It mirrors
// publicKeyFor's resolution order — an org client first, then a global user —
// and records whether a global user is an admin (Chef's pivotal superuser).
func (s *Server) resolveActor(path, name string) api.Actor {
	if org := orgFromPath(path); org != "" {
		if o, ok := s.store.Org(org); ok {
			if _, ok := o.Get("clients", name); ok {
				return api.Actor{Name: name, IsClient: true}
			}
		}
	}
	actor := api.Actor{Name: name}
	if raw, ok := s.store.Global().Get("users", name); ok {
		var u struct {
			Admin bool `json:"admin"`
		}
		if json.Unmarshal(raw, &u) == nil {
			actor.IsGlobalAdmin = u.Admin
		}
	}
	return actor
}

// checkSkew rejects timestamps outside the allowed clock-skew window.
func (s *Server) checkSkew(timestamp string) error {
	ts, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return skewError{"invalid X-Ops-Timestamp"}
	}
	delta := math.Abs(s.opts.Now().Sub(ts).Seconds())
	if delta > float64(s.opts.SkewSeconds) {
		return skewError{"Original request time stamp too far in the future or past."}
	}
	return nil
}

type skewError struct{ msg string }

func (e skewError) Error() string { return e.msg }

func unauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": []string{msg}})
}

// isFileStorePath reports whether path addresses the cookbook file store
// (/organizations/{org}/file_store/{checksum}), which is served without auth.
func isFileStorePath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 4 && parts[0] == "organizations" && parts[2] == "file_store"
}

// orgFromPath extracts the organization name from an "/organizations/{org}/..."
// request path, or "" if the path is not org-scoped.
func orgFromPath(path string) string {
	const prefix = "/organizations/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := path[len(prefix):]
	org, _, _ := strings.Cut(rest, "/")
	return org
}

// actorKey reads an actor's public_key field from a store collection and parses
// it into an RSA public key.
func actorKey(org *store.Org, collection, name string) (*rsa.PublicKey, bool) {
	raw, ok := org.Get(collection, name)
	if !ok {
		return nil, false
	}
	var obj struct {
		PublicKey string `json:"public_key"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil || obj.PublicKey == "" {
		return nil, false
	}
	pub, err := auth.ParsePublicKey([]byte(obj.PublicKey))
	if err != nil {
		return nil, false
	}
	return pub, true
}
