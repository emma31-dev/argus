package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

func urlPathUnescape(s string) (string, error) { return url.PathUnescape(s) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Del("Cache-Control")
	writeJSON(w, status, map[string]string{"error": msg})
}

// cacheFor sets a short public cache lifetime.
func cacheFor(w http.ResponseWriter, seconds int) {
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(seconds))
}
