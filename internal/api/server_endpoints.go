package api

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// Assorted server endpoints: /_stats, per-org required_recipe and principals,
// plus the X-Ops-Server-API-Version negotiation applied to every response.

// Supported server API version range advertised to clients.
const (
	apiVersionMin = 0
	apiVersionMax = 2
)

func (a *API) registerServerEndpoints(mux *http.ServeMux) {
	mux.HandleFunc("GET /_stats", a.stats)
	mux.HandleFunc("GET /organizations/{org}/required_recipe", a.requiredRecipe)
	mux.HandleFunc("GET /organizations/{org}/principals/{name}", a.principal)
}

// stats is a stub: a stock Chef server exposes erchef/Prometheus metrics here,
// which carry no meaning for an in-memory test server, so it returns an empty
// (but well-formed) stats array.
func (a *API) stats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []any{})
}

// requiredRecipe mirrors a stock server where the feature is disabled.
func (a *API) requiredRecipe(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "Required recipe is not configured on this server.")
}

// principal resolves a name to its actor record. Org clients take precedence
// over global users, matching Chef's principal lookup.
func (a *API) principal(w http.ResponseWriter, r *http.Request) {
	org := a.org(w, r)
	if org == nil {
		return
	}
	name := r.PathValue("name")

	if raw, ok := org.Get("clients", name); ok {
		writeJSON(w, http.StatusOK, principalDoc(name, "client", raw))
		return
	}
	if raw, ok := a.store.Global().Get("users", name); ok {
		writeJSON(w, http.StatusOK, principalDoc(name, "user", raw))
		return
	}
	writeError(w, http.StatusNotFound, "Cannot find principal "+name)
}

func principalDoc(name, typ string, actorRaw []byte) map[string]any {
	var actor map[string]any
	json.Unmarshal(actorRaw, &actor)
	pubKey, _ := actor["public_key"].(string)
	return map[string]any{
		"name":       name,
		"type":       typ,
		"public_key": pubKey,
		"authz_id":   name,
		"org_member": true,
	}
}

// withAPIVersion advertises the supported server API version range on every
// response (X-Ops-Server-API-Version) and rejects requests asking for a version
// outside that range with 406, as Chef clients expect for version negotiation.
func withAPIVersion(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested := apiVersionMin
		if v := r.Header.Get("X-Ops-Server-API-Version"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				requested = n
			}
		}
		response := min(max(requested, apiVersionMin), apiVersionMax)

		negotiation, _ := json.Marshal(map[string]string{
			"min_version":      strconv.Itoa(apiVersionMin),
			"max_version":      strconv.Itoa(apiVersionMax),
			"request_version":  strconv.Itoa(requested),
			"response_version": strconv.Itoa(response),
		})
		w.Header().Set("X-Ops-Server-API-Version", string(negotiation))

		if requested < apiVersionMin || requested > apiVersionMax {
			writeError(w, http.StatusNotAcceptable,
				"Specified version not supported by this server.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
