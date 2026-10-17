package api

import "net/http"

// CustomHeader must be present on every mutating request. It is not
// CORS-safelisted, so a cross-site browser request carrying it has to pass a
// preflight, which only the configured CORS origin does. The API has no
// authentication, so this is what keeps other websites from retriggering,
// marking or deleting events through a visitor's browser.
const CustomHeader = "X-Requested-With"

// CORSMiddleware adds CORS headers and handles OPTIONS preflight.
// If corsOrigin is empty, CORS is disabled (safest for cluster-internal APIs).
func CORSMiddleware(next http.Handler, corsOrigin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only set CORS headers if a specific origin is configured
		if corsOrigin != "" {
			w.Header().Set("Access-Control-Allow-Origin", corsOrigin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, "+CustomHeader)
			w.Header().Set("Vary", "Origin")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RequireCustomHeader rejects POST, PATCH and DELETE requests that lack
// CustomHeader with 403. Reads and preflights pass through.
func RequireCustomHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPatch, http.MethodDelete:
			if r.Header.Get(CustomHeader) == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"missing X-Requested-With header"}`))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
