package middleware

import (
	"net/http"
	"strings"
)

// Auth returns middleware that validates API key from Authorization header or x-api-key.
func Auth(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for health endpoints
			if r.URL.Path == "/" || r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}

			key := extractAPIKey(r)
			if key == "" || key != apiKey {
				http.Error(w, `{"error":{"message":"Invalid API key","type":"authentication_error"}}`, http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func extractAPIKey(r *http.Request) string {
	// OpenAI style: Authorization: Bearer <key>
	if auth := r.Header.Get("Authorization"); auth != "" {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	// Anthropic style: x-api-key: <key>
	if key := r.Header.Get("x-api-key"); key != "" {
		return key
	}
	return ""
}
