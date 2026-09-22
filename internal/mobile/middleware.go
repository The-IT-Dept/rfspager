package mobile

import (
	"context"
	"net/http"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/authjwt"
)

type contextKey string

const UserContextKey contextKey = "mobile_user"

type AuthUser struct {
	ID                    int64   `db:"id" json:"id"`
	UserID                string  `db:"user_id" json:"user_id"`
	Name                  *string `db:"name" json:"name"`
	Email                 string  `db:"email" json:"email"`
	SubscriptionValid     bool    `db:"subscription_valid" json:"subscription_valid"`
	SubscriptionProductID *string `db:"subscription_product_id" json:"subscription_product_id"`
	SubscriptionExpiry    *string `db:"subscription_expiry" json:"subscription_expiry"`
	WillRenew             *bool   `db:"will_renew" json:"will_renew"`
	SubscriptionPlatform  *string `db:"subscription_platform" json:"subscription_platform"`
	Superuser             bool    `db:"superuser" json:"-"`
	TokenVersion          int64   `db:"token_version" json:"-"`
}

func JWTUserAuth(db *sqlx.DB, jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}

			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
			if tokenStr == authHeader {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid authorization header"})
				return
			}

			claims, err := authjwt.Validate([]byte(jwtSecret), tokenStr, "rfspager-mobile", "mobile")
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
				return
			}

			var user AuthUser
			err = db.GetContext(r.Context(), &user,
				`SELECT id, user_id, name, email, subscription_valid,
				 subscription_product_id, subscription_expiry, will_renew,
				 subscription_platform, superuser, token_version
				 FROM users WHERE id = $1`, claims.Subject)
			if err != nil || user.TokenVersion != claims.Version {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "user not found"})
				return
			}

			ctx := context.WithValue(r.Context(), UserContextKey, &user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetUser(ctx context.Context) *AuthUser {
	u, _ := ctx.Value(UserContextKey).(*AuthUser)
	return u
}

// WithStripeContext bridges mobile auth user into stripe handler context.
func WithStripeContext(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r.Context())
		if user == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		ctx := context.WithValue(r.Context(), StripeUserIDKey, user.ID)
		ctx = context.WithValue(ctx, StripeUserEmailKey, user.Email)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// Exported context keys for stripe handler to read
const StripeUserIDKey contextKey = "stripe_user_id"
const StripeUserEmailKey contextKey = "stripe_user_email"
