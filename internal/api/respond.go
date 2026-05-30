package api

import (
	"encoding/json"
	"net/http"
)

// writeJSON writes v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// writeRaw writes pre-encoded JSON bytes with the given status code.
func writeRaw(w http.ResponseWriter, status int, raw []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

// writeError writes a Chef-style error body: {"error":["message", ...]}.
func writeError(w http.ResponseWriter, status int, messages ...string) {
	writeJSON(w, status, map[string]any{"error": messages})
}

// requestBaseURL reconstructs the scheme://host prefix for building the
// absolute URLs Chef returns in list and create responses.
func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	return scheme + "://" + r.Host
}
