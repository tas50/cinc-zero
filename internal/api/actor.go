package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/tas50/cinc-zero/internal/auth"
	"github.com/tas50/cinc-zero/internal/store"
)

// scopeFunc resolves the store space an actor collection lives in. Clients are
// org-scoped; users are global.
type scopeFunc func(w http.ResponseWriter, r *http.Request) *store.Org

// registerActorRoutes wires CRUD for an "actor" collection (clients or users).
// Actors are JSON objects carrying a public_key; on creation the server
// generates an RSA key pair if the caller did not supply one, returning the
// private key exactly once.
func (a *API) registerActorRoutes(mux *http.ServeMux, prefix, segment string, scope scopeFunc) {
	base := prefix + segment
	mux.HandleFunc("GET "+base, a.scopedList(segment, scope))
	mux.HandleFunc("POST "+base, a.createActor(segment, scope))
	mux.HandleFunc("GET "+base+"/{name}", a.scopedGet(segment, scope))
	mux.HandleFunc("PUT "+base+"/{name}", a.scopedPut(segment, scope))
	mux.HandleFunc("DELETE "+base+"/{name}", a.scopedDelete(segment, scope))
}

func (a *API) createActor(segment string, scope scopeFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		org := scope(w, r)
		if org == nil {
			return
		}
		var obj map[string]any
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		name, _ := obj["name"].(string)
		if name == "" {
			// users may use "username" as the identity field
			name, _ = obj["username"].(string)
		}
		if name == "" {
			writeError(w, http.StatusBadRequest, "Field 'name' missing")
			return
		}

		resp := map[string]any{
			"uri": objectURL(r, orgSegment(r), segment, name),
		}

		// Generate a key pair unless the caller supplied a public key.
		if _, hasPub := obj["public_key"].(string); !hasPub {
			key, err := auth.GenerateKey()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "key generation failed")
				return
			}
			pubPEM, err := auth.EncodePublicKeyPEM(&key.PublicKey)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "key encoding failed")
				return
			}
			obj["public_key"] = string(pubPEM)
			resp["private_key"] = string(auth.EncodePrivateKeyPEM(key))
			resp["public_key"] = string(pubPEM)
		}
		// The private key is never persisted; a password is stored out-of-band
		// (for authenticate_user) and stripped from the object.
		delete(obj, "private_key")
		stashPassword(org, name, obj)

		raw := mustEncode(obj)
		if err := org.Create(segment, name, raw); errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "Object already exists")
			return
		}
		writeJSON(w, http.StatusCreated, resp)
	}
}

// orgSegment returns the org path value, or "" for global (user) routes.
func orgSegment(r *http.Request) string {
	return r.PathValue("org")
}

func (a *API) scopedList(segment string, scope scopeFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		org := scope(w, r)
		if org == nil {
			return
		}
		out := map[string]string{}
		for _, name := range org.Keys(segment) {
			out[name] = objectURL(r, orgSegment(r), segment, name)
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func (a *API) scopedGet(segment string, scope scopeFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		org := scope(w, r)
		if org == nil {
			return
		}
		raw, ok := org.Get(segment, r.PathValue("name"))
		if !ok {
			writeError(w, http.StatusNotFound, "Cannot find "+segment+" "+r.PathValue("name"))
			return
		}
		writeRaw(w, http.StatusOK, raw)
	}
}

func (a *API) scopedPut(segment string, scope scopeFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		org := scope(w, r)
		if org == nil {
			return
		}
		name := r.PathValue("name")
		var obj map[string]any
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		delete(obj, "private_key")
		stashPassword(org, name, obj)
		raw := mustEncode(obj)
		org.Put(segment, name, raw)
		writeRaw(w, http.StatusOK, raw)
	}
}

func (a *API) scopedDelete(segment string, scope scopeFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		org := scope(w, r)
		if org == nil {
			return
		}
		raw, ok := org.Delete(segment, r.PathValue("name"))
		if !ok {
			writeError(w, http.StatusNotFound, "Cannot find "+segment+" "+r.PathValue("name"))
			return
		}
		writeRaw(w, http.StatusOK, raw)
	}
}

// mustEncode marshals v to canonical JSON without HTML escaping.
func mustEncode(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return bytes.TrimRight(buf.Bytes(), "\n")
}
