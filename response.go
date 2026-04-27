package main

import "net/http"

type responseGuardWriter struct {
	http.ResponseWriter
	method string
	status int
	wrote  bool
}

func (w *responseGuardWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.status = status
	w.wrote = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseGuardWriter) Write(data []byte) (int, error) {
	if !w.wrote {
		w.status = http.StatusOK
		if !bodyAllowed(w.method, w.status) {
			w.WriteHeader(w.status)
			return len(data), nil
		}
		w.wrote = true
	}

	if !bodyAllowed(w.method, w.status) {
		return len(data), nil
	}

	return w.ResponseWriter.Write(data)
}

func (w *responseGuardWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *responseGuardWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func bodyAllowed(method string, status int) bool {
	if method == http.MethodHead {
		return false
	}
	if status >= 100 && status < 200 {
		return false
	}
	return status != http.StatusNoContent && status != http.StatusNotModified
}

// ResponseGuard smooths over response edge-cases that are valid API outcomes
// but noisy with net/http, like attempting to write a 304 error body.
func ResponseGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&responseGuardWriter{
			ResponseWriter: w,
			method:         r.Method,
		}, r)
	})
}
