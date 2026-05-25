package middleware

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"

	"github.com/the-it-dept/rfspager/internal/db"
	"github.com/the-it-dept/rfspager/internal/models"
)

type contextKey string

const SystemContextKey contextKey = "system"

// SanctumAuth validates a Bearer token against Laravel's personal_access_tokens table.
// Laravel Sanctum stores SHA-256 hashed tokens.
func SanctumAuth(database *db.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token == authHeader {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			// Sanctum tokens are stored as "id|plaintext" - we need to find and verify
			parts := strings.SplitN(token, "|", 2)
			if len(parts) != 2 {
				http.Error(w, `{"error":"invalid token format"}`, http.StatusUnauthorized)
				return
			}

			// Hash the plaintext part with SHA-256 (how Sanctum stores it)
			hash := sha256.Sum256([]byte(parts[1]))
			hashedToken := fmt.Sprintf("%x", hash)

			system, err := database.GetSystemBySanctumToken(r.Context(), parts[0], hashedToken)
			if err != nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), SystemContextKey, system)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetSystem extracts the system from the request context.
func GetSystem(ctx context.Context) *models.System {
	s, _ := ctx.Value(SystemContextKey).(*models.System)
	return s
}
