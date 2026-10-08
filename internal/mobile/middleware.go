package mobile

import (
	"context"
	"net/http"
	"strings"
	"time"

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
			serveAsUser(w, r, next, db, claims)
		})
	}
}

// serveAsUser loads the token's user, checks the token hasn't been revoked
// (token_version bumps on password change / logout everywhere), and serves
// the request with the user in context.
func serveAsUser(w http.ResponseWriter, r *http.Request, next http.Handler, db *sqlx.DB, claims authjwt.Claims) {
	var user AuthUser
	err := db.GetContext(r.Context(), &user,
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
}

// Stream tokens let a browser reach the radio audio and event streams:
// <audio src> and EventSource can't send an Authorization header, so the
// page swaps its login token for a short-lived, radio-only one it can put in
// the URL. A leaked stream token expires in minutes and opens nothing else.
const (
	streamTokenType     = "radio-stream"
	streamTokenLifetime = 15 * time.Minute
)

// issueStreamToken mints a stream token for user.
func issueStreamToken(jwtSecret string, user *AuthUser) (string, time.Time, error) {
	exp := time.Now().Add(streamTokenLifetime)
	tok, err := authjwt.Issue([]byte(jwtSecret), "rfspager-mobile", streamTokenType, user.ID, user.TokenVersion, streamTokenLifetime, nil)
	return tok, exp, err
}

// StreamOrBearerAuth accepts either the normal Bearer login token or a
// ?token= stream token. Mount it only on the routes browsers load directly.
func StreamOrBearerAuth(db *sqlx.DB, jwtSecret string) func(http.Handler) http.Handler {
	bearer := JWTUserAuth(db, jwtSecret)
	return func(next http.Handler) http.Handler {
		viaBearer := bearer(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok := r.URL.Query().Get("token")
			if r.Header.Get("Authorization") != "" || tok == "" {
				viaBearer.ServeHTTP(w, r)
				return
			}
			claims, err := authjwt.Validate([]byte(jwtSecret), tok, "rfspager-mobile", streamTokenType)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
				return
			}
			serveAsUser(w, r, next, db, claims)
		})
	}
}

// RequireSubscription gates a paid feature. Mount it after JWTUserAuth.
// Superusers pass, like a subscriber.
//
// Non-subscribers get 402 Payment Required with code "subscription_required".
// Not 401: the app's API client logs the user out on any 401. Not 403: that
// already means "not yours" elsewhere in this API (system ownership). 402
// means exactly "subscribe to see this", so clients can show the paywall on
// it without guessing.
func RequireSubscription(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r.Context())
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		if !user.SubscriptionValid && !user.Superuser {
			writeJSON(w, http.StatusPaymentRequired, map[string]string{
				"error": "an active subscription is required",
				"code":  "subscription_required",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
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
