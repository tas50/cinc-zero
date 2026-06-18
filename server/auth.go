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

		// Management-console impersonation: when a request is sourced from the
		// web UI, its signature is made with the webui key (not the actor's own
		// key) and X-Ops-Userid names the user being acted for. Verify against
		// the webui key and run as that user. This mirrors Chef Infra Server's
		// webui-key mechanism, which a console uses to honor each user's ACLs.
		if strings.EqualFold(r.Header.Get("X-Ops-Request-Source"), "web") {
			if err := auth.Verify(r.Method, r.URL.Path, body, parsed, r.Header.Get("X-Ops-Server-API-Version"), s.webuiPub); err != nil {
				unauthorized(w, "Invalid webui signature for request source 'web'")
				return
			}
			_, actor, ok := s.resolveAuth(r.URL.Path, parsed.UserID)
			if !ok {
				unauthorized(w, "Failed to authenticate as '"+parsed.UserID+"' via the web UI. Ensure the user exists.")
				return
			}
			actor.ViaWebUI = true
			if err := s.checkSkew(parsed.Timestamp); err != nil {
				unauthorized(w, err.Error())
				return
			}
			r = r.WithContext(api.WithActor(r.Context(), actor))
			next.ServeHTTP(w, r)
			return
		}

		// One lookup resolves both the signing key and the actor identity from a
		// single store read of the actor's record.
		pub, actor, ok := s.resolveAuth(r.URL.Path, parsed.UserID)
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
		r = r.WithContext(api.WithActor(r.Context(), actor))
		next.ServeHTTP(w, r)
	})
}

// resolveAuth resolves an actor's signing key and identity in a single store
// read of its record. It checks org clients first (when the request targets an
// org), then global users, mirroring Chef's resolution order. The actor records
// whether it is an org client (vs. a global user) and whether a global user is
// an admin (Chef's pivotal superuser, which bypasses ACLs).
func (s *Server) resolveAuth(path, name string) (*rsa.PublicKey, api.Actor, bool) {
	if org := orgFromPath(path); org != "" {
		if o, ok := s.store.Org(org); ok {
			if pub, _, ok := s.parseActorRecord(o, "clients", name); ok {
				return pub, api.Actor{Name: name, IsClient: true}, true
			}
		}
	}
	if pub, admin, ok := s.parseActorRecord(s.store.Global(), "users", name); ok {
		return pub, api.Actor{Name: name, IsGlobalAdmin: admin}, true
	}
	return nil, api.Actor{}, false
}

// parseWebUIKey accepts a PEM-encoded RSA public or private key and returns the
// public key used to verify web-sourced (management-console) requests.
func parseWebUIKey(pemBytes []byte) (*rsa.PublicKey, error) {
	if pub, err := auth.ParsePublicKey(pemBytes); err == nil {
		return pub, nil
	}
	priv, err := auth.ParsePrivateKey(pemBytes)
	if err != nil {
		return nil, err
	}
	return &priv.PublicKey, nil
}

// parseActorRecord reads an actor record from a store collection and extracts
// its RSA public key (parsed through the server's key cache to avoid re-parsing
// PEM/x509 on every request) and its admin flag. The record is read copy-free
// since it is only unmarshalled here.
func (s *Server) parseActorRecord(org *store.Org, collection, name string) (pub *rsa.PublicKey, admin, ok bool) {
	raw, found := org.View(collection, name)
	if !found {
		return nil, false, false
	}
	var rec struct {
		PublicKey string `json:"public_key"`
		Admin     bool   `json:"admin"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil || rec.PublicKey == "" {
		return nil, false, false
	}
	key, err := s.keyCache.Parse(rec.PublicKey)
	if err != nil {
		return nil, false, false
	}
	return key, rec.Admin, true
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
