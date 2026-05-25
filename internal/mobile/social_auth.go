package mobile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/MicahParks/keyfunc"
	"github.com/golang-jwt/jwt/v4"
)

// SocialLogin handles POST /api/v2/mobile/auth/social-login
// Body: { "provider": "apple"|"facebook", "token": "...", "name": "...", "email": "..." }
// `name` and `email` are optional fallbacks used on first Apple sign-in (Apple
// only surfaces the email/name on the very first auth and won't include them
// in the identity token on subsequent sign-ins).
func (h *AuthHandler) SocialLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Token    string `json:"token"`
		Name     string `json:"name,omitempty"`
		Email    string `json:"email,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "token required"})
		return
	}

	var (
		providerID string
		email      string
		name       string
		err        error
	)

	switch strings.ToLower(req.Provider) {
	case "apple":
		providerID, email, err = verifyAppleIdentityToken(req.Token, h.appleBundleID)
	case "facebook":
		providerID, email, name, err = verifyFacebookAccessToken(r.Context(), req.Token)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported provider"})
		return
	}
	if err != nil {
		slog.Warn("social-login: token verification failed", "provider", req.Provider, "error", err)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
		return
	}

	// Apple omits email after the first sign-in; fall back to what the client supplied.
	if email == "" {
		email = req.Email
	}
	if name == "" {
		name = req.Name
	}

	user, err := h.findOrCreateSocialUser(r, req.Provider, providerID, email, name)
	if err != nil {
		slog.Error("social-login: user lookup/create failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	token, err := h.generateToken(user)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	writeJSON(w, http.StatusOK, authResponse{Token: token, User: *user})
}

// findOrCreateSocialUser looks up by (provider, provider_id), then by email,
// and creates a new user if neither match. It also ensures a social_providers
// row is recorded for the user.
func (h *AuthHandler) findOrCreateSocialUser(r *http.Request, provider, providerID, email, name string) (*AuthUser, error) {
	ctx := r.Context()
	provider = strings.ToLower(provider)

	user := &AuthUser{}
	loadUser := func(id int64) error {
		return h.db.QueryRowContext(ctx,
			`SELECT id, user_id, name, email, subscription_valid,
			 subscription_product_id, subscription_expiry, will_renew,
			 subscription_platform, superuser
			 FROM users WHERE id = $1`, id).Scan(
			&user.ID, &user.UserID, &user.Name, &user.Email,
			&user.SubscriptionValid, &user.SubscriptionProductID,
			&user.SubscriptionExpiry, &user.WillRenew, &user.SubscriptionPlatform,
			&user.Superuser)
	}

	// 1. Existing social link.
	var existingUserID int64
	err := h.db.QueryRowContext(ctx,
		`SELECT user_id FROM social_providers WHERE provider = $1 AND provider_id = $2`,
		provider, providerID).Scan(&existingUserID)
	if err == nil {
		if err := loadUser(existingUserID); err != nil {
			return nil, fmt.Errorf("loading linked user: %w", err)
		}
		return user, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("querying social_providers: %w", err)
	}

	// 2. Existing user by email — link the social account.
	if email != "" {
		var id int64
		err := h.db.QueryRowContext(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&id)
		if err == nil {
			if _, err := h.db.ExecContext(ctx,
				`INSERT INTO social_providers (user_id, provider, provider_id, created_at, updated_at)
				 VALUES ($1, $2, $3, NOW(), NOW())`, id, provider, providerID); err != nil {
				return nil, fmt.Errorf("linking social provider: %w", err)
			}
			if err := loadUser(id); err != nil {
				return nil, fmt.Errorf("loading user by email: %w", err)
			}
			return user, nil
		}
		if err != sql.ErrNoRows {
			return nil, fmt.Errorf("querying users by email: %w", err)
		}
	}

	// 3. Brand-new user. Email is required for create.
	if email == "" {
		return nil, errors.New("provider did not return an email and none was supplied")
	}
	if name == "" {
		if at := strings.IndexByte(email, '@'); at > 0 {
			name = email[:at]
		} else {
			name = email
		}
	}

	var newID int64
	if err := h.db.QueryRowContext(ctx,
		`INSERT INTO users (user_id, name, email, email_verified_at, created_at, updated_at)
		 VALUES (gen_random_uuid(), $1, $2, NOW(), NOW(), NOW())
		 RETURNING id`, name, email).Scan(&newID); err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}
	if _, err := h.db.ExecContext(ctx,
		`INSERT INTO social_providers (user_id, provider, provider_id, created_at, updated_at)
		 VALUES ($1, $2, $3, NOW(), NOW())`, newID, provider, providerID); err != nil {
		return nil, fmt.Errorf("recording social provider: %w", err)
	}
	if err := loadUser(newID); err != nil {
		return nil, fmt.Errorf("loading new user: %w", err)
	}
	return user, nil
}

// --- Apple identity token verification -------------------------------------

var (
	appleJWKSOnce sync.Once
	appleJWKS     *keyfunc.JWKS
	appleJWKSErr  error
)

func getAppleJWKS() (*keyfunc.JWKS, error) {
	appleJWKSOnce.Do(func() {
		appleJWKS, appleJWKSErr = keyfunc.Get("https://appleid.apple.com/auth/keys", keyfunc.Options{
			RefreshInterval:   24 * time.Hour,
			RefreshTimeout:    10 * time.Second,
			RefreshErrorHandler: func(err error) {
				slog.Error("apple JWKS refresh failed", "error", err)
			},
		})
	})
	return appleJWKS, appleJWKSErr
}

// verifyAppleIdentityToken validates an Apple Sign In identity token against
// Apple's JWKS and the expected audience (bundle ID). Returns (sub, email).
func verifyAppleIdentityToken(idToken, audience string) (string, string, error) {
	jwks, err := getAppleJWKS()
	if err != nil {
		return "", "", fmt.Errorf("loading Apple JWKS: %w", err)
	}

	token, err := jwt.Parse(idToken, jwks.Keyfunc)
	if err != nil {
		return "", "", fmt.Errorf("parsing Apple JWT: %w", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return "", "", errors.New("invalid Apple JWT")
	}

	if iss, _ := claims["iss"].(string); iss != "https://appleid.apple.com" {
		return "", "", fmt.Errorf("unexpected issuer: %s", iss)
	}
	if audience != "" {
		switch aud := claims["aud"].(type) {
		case string:
			if aud != audience {
				return "", "", fmt.Errorf("unexpected audience: %s", aud)
			}
		case []any:
			matched := false
			for _, v := range aud {
				if s, _ := v.(string); s == audience {
					matched = true
					break
				}
			}
			if !matched {
				return "", "", errors.New("audience does not include bundle ID")
			}
		}
	}

	sub, _ := claims["sub"].(string)
	if sub == "" {
		return "", "", errors.New("missing sub claim")
	}
	email, _ := claims["email"].(string)
	return sub, email, nil
}

// --- Facebook access token verification ------------------------------------

// verifyFacebookAccessToken hits the Graph API. Returns (provider_id, email, name).
func verifyFacebookAccessToken(ctx context.Context, accessToken string) (string, string, string, error) {
	u := "https://graph.facebook.com/me?fields=id,email,name&access_token=" + url.QueryEscape(accessToken)
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("calling FB graph API: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("FB graph API %d: %s", resp.StatusCode, string(body))
	}
	var data struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", "", "", fmt.Errorf("decoding FB response: %w", err)
	}
	if data.ID == "" {
		return "", "", "", errors.New("FB returned no user id")
	}
	return data.ID, data.Email, data.Name, nil
}
