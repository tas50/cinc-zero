package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/tas50/cinc-zero/internal/store"
)

// registerObjectRoutes wires the standard Chef CRUD routes for a "named JSON
// object" collection such as nodes, roles, or environments. These share an
// identical shape: a list endpoint returning name->URL, plus create/get/
// update/delete/head on individual named objects.
func (a *API) registerObjectRoutes(mux *http.ServeMux, segment string) {
	base := "/organizations/{org}/" + segment
	mux.HandleFunc("GET "+base, a.listObjects(segment))
	mux.HandleFunc("POST "+base, a.createObject(segment))
	mux.HandleFunc("GET "+base+"/{name}", a.getObject(segment))
	mux.HandleFunc("PUT "+base+"/{name}", a.putObject(segment))
	mux.HandleFunc("DELETE "+base+"/{name}", a.deleteObject(segment))
	mux.HandleFunc("HEAD "+base+"/{name}", a.headObject(segment))
}

// objectURL builds the absolute URL for a named object.
func objectURL(r *http.Request, org, segment, name string) string {
	return requestBaseURL(r) + "/organizations/" + org + "/" + segment + "/" + name
}

func (a *API) listObjects(segment string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		org := a.org(w, r)
		if org == nil {
			return
		}
		out := map[string]string{}
		for _, name := range org.Keys(segment) {
			out[name] = objectURL(r, org.Name(), segment, name)
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// decodeNamedBody reads the request body as a JSON object and returns the
// canonical bytes plus the value of its "name" field.
func decodeNamedBody(r *http.Request) (raw []byte, name string, err error) {
	var obj map[string]any
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&obj); err != nil {
		return nil, "", err
	}
	n, _ := obj["name"].(string)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(obj); err != nil {
		return nil, "", err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), n, nil
}

func (a *API) createObject(segment string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		org := a.org(w, r)
		if org == nil {
			return
		}
		raw, name, err := decodeNamedBody(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if name == "" {
			writeError(w, http.StatusBadRequest, "Field 'name' missing")
			return
		}
		if err := org.Create(segment, name, raw); errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "Object already exists")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{
			"uri": objectURL(r, org.Name(), segment, name),
		})
	}
}

func (a *API) getObject(segment string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		org := a.org(w, r)
		if org == nil {
			return
		}
		name := r.PathValue("name")
		raw, ok := org.View(segment, name)
		if !ok {
			writeError(w, http.StatusNotFound, "Cannot find "+segment+" "+name)
			return
		}
		writeRaw(w, http.StatusOK, raw)
	}
}

func (a *API) putObject(segment string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		org := a.org(w, r)
		if org == nil {
			return
		}
		name := r.PathValue("name")
		raw, _, err := decodeNamedBody(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		org.Put(segment, name, raw)
		writeRaw(w, http.StatusOK, raw)
	}
}

func (a *API) deleteObject(segment string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		org := a.org(w, r)
		if org == nil {
			return
		}
		name := r.PathValue("name")
		raw, ok := org.Delete(segment, name)
		if !ok {
			writeError(w, http.StatusNotFound, "Cannot find "+segment+" "+name)
			return
		}
		writeRaw(w, http.StatusOK, raw)
	}
}

func (a *API) headObject(segment string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		org := a.org(w, r)
		if org == nil {
			return
		}
		if _, ok := org.View(segment, r.PathValue("name")); !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}
