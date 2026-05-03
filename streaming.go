package main

import (
	"net/http"
	"strings"
)

var streamingPaths = []string{"/sse/metrics", "/events", "/logs", "/stream-bytes", "/drip"}

func isStreamingPath(path string) bool {
	for _, streamPath := range streamingPaths {
		if strings.HasPrefix(path, streamPath) {
			return true
		}
	}
	return false
}

// StreamingHeaders marks streaming responses so intermediaries do not buffer,
// transform, or cache them before forwarding data to clients.
func StreamingHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isStreamingPath(r.URL.Path) {
			header := w.Header()
			header.Set("Cache-Control", "no-cache, no-store, no-transform")
			header.Set("Surrogate-Control", "no-store")
			header.Set("X-Accel-Buffering", "no")
		}

		next.ServeHTTP(w, r)
	})
}
