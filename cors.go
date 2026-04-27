package main

import "net/http"

const corsAllowMethods = "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS"

// CORS allows browser-based clients, including the interactive docs, to call
// this API from any origin.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("Access-Control-Allow-Origin", "*")
		header.Set("Access-Control-Expose-Headers", "*")

		if r.Header.Get("Origin") != "" && r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			header.Set("Access-Control-Allow-Methods", corsAllowMethods)
			header.Set("Access-Control-Max-Age", "86400")
			header.Add("Vary", "Origin")
			header.Add("Vary", "Access-Control-Request-Method")
			header.Add("Vary", "Access-Control-Request-Headers")

			if requestedHeaders := r.Header.Get("Access-Control-Request-Headers"); requestedHeaders != "" {
				header.Set("Access-Control-Allow-Headers", requestedHeaders)
			} else {
				header.Set("Access-Control-Allow-Headers", "*")
			}

			if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
				header.Set("Access-Control-Allow-Private-Network", "true")
			}

			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
