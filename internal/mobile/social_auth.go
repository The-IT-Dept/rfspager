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

type providerIdentity struct {
	ID            string
	Email         string
	Name          string
	EmailVerified bool
}

var errSocialIdentityAlreadyOwned = errors.New("social identity requires authenticated account linking")

// SocialLogin handles POST /api/v2/mobile/auth/social-login. Provider identity
// data is authoritative; client-supplied email is intentionally ignored.
func (h *AuthHandler) SocialLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Token    string `json:"token"`
		Name     string `json:"name,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "token required"})
		return
	}

	var identity providerIdentity
	var err error

	switch strings.ToLower(req.Provider) {
	case "apple":
		identity, err = h.appleVerifier(req.Token, h.appleBundleID)
	case "facebook":
		identity, err = h.facebookVerifier(r.Context(), req.Token)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported provider"})
		return
	}
	if err != nil {
		slog.Warn("social-login: token verification failed", "provider", req.Provider, "error", err)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
		return
	}

	if identity.Name == "" {
		identity.Name = req.Name
	}

	user, err := h.findOrCreateSocialUser(r, req.Provider, identity)
	if err != nil {
		if errors.Is(err, errSocialIdentityAlreadyOwned) {
			slog.Info("social-login: refused, email belongs to an account not linked to this identity", "provider", strings.ToLower(req.Provider))
			writeJSON(w, http.StatusConflict, map[string]string{"error": "social identity must be linked from an authenticated account"})
			return
		}
		if errors.Is(err, errProviderEmailNotVerified) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "provider did not return a verified email"})
			return
		}
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

var errProviderEmailNotVerified = errors.New("provider did not return a verified email")

// findOrCreateSocialUser permits an existing provider link. It links a
// provider to an existing account by email only for Apple (see linkAppleByEmail);
// other providers must be linked from an authenticated session.
func (h *AuthHandler) findOrCreateSocialUser(r *http.Request, provider string, identity providerIdentity) (*AuthUser, error) {
	ctx := r.Context()
	provider = strings.ToLower(provider)

	user := &AuthUser{}
	loadUser := func(id int64) error {
		return h.db.QueryRowContext(ctx,
			`SELECT id, user_id, name, email, subscription_valid,
			 subscription_product_id, subscription_expiry, will_renew,
			 subscription_platform, superuser, token_version
			 FROM users WHERE id = $1`, id).Scan(
			&user.ID, &user.UserID, &user.Name, &user.Email,
			&user.SubscriptionValid, &user.SubscriptionProductID,
			&user.SubscriptionExpiry, &user.WillRenew, &user.SubscriptionPlatform,
			&user.Superuser, &user.TokenVersion)
	}

	// 1. Existing social link.
	var existingUserID int64
	err := h.db.QueryRowContext(ctx,
		`SELECT user_id FROM social_providers WHERE provider = $1 AND provider_id = $2`,
		provider, identity.ID).Scan(&existingUserID)
	if err == nil {
		if err := loadUser(existingUserID); err != nil {
			return nil, fmt.Errorf("loading linked user: %w", err)
		}
		return user, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("querying social_providers: %w", err)
	}

	// 2. A verified provider email is required to create a new identity.
	if !identity.EmailVerified || identity.Email == "" {
		return nil, errProviderEmailNotVerified
	}
	{
		var id int64
		err := h.db.QueryRowContext(ctx, `SELECT id FROM users WHERE lower(email) = lower($1) `+emailMatchOrder+` LIMIT 1`, identity.Email).Scan(&id)
		switch {
		case err == nil && provider == "apple":
			if err := h.linkAppleByEmail(ctx, id, identity.ID); err != nil {
				return nil, err
			}
			if err := loadUser(id); err != nil {
				return nil, fmt.Errorf("loading linked user: %w", err)
			}
			return user, nil
		case err == nil:
			return nil, errSocialIdentityAlreadyOwned
		case err != sql.ErrNoRows:
			return nil, fmt.Errorf("querying users by email: %w", err)
		}
	}

	// 3. Brand-new user with an authenticated provider email.
	if identity.Name == "" {
		if at := strings.IndexByte(identity.Email, '@'); at > 0 {
			identity.Name = identity.Email[:at]
		} else {
			identity.Name = identity.Email
		}
	}

	var newID int64
	if err := h.db.QueryRowContext(ctx,
		`INSERT INTO users (user_id, name, email, password, email_verified_at, created_at, updated_at)
		 VALUES (gen_random_uuid(), $1, $2, '!', NOW(), NOW(), NOW())
		 RETURNING id`, identity.Name, identity.Email).Scan(&newID); err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}
	if _, err := h.db.ExecContext(ctx,
		`INSERT INTO social_providers (user_id, provider, provider_id, created_at, updated_at)
		 VALUES ($1, $2, $3, NOW(), NOW())`, newID, provider, identity.ID); err != nil {
		return nil, fmt.Errorf("recording social provider: %w", err)
	}
	if err := loadUser(newID); err != nil {
		return nil, fmt.Errorf("loading new user: %w", err)
	}
	return user, nil
}

// linkAppleByEmail links an Apple ID to the existing account holding the
// email Apple verified. Apple checks that email itself, and a Hide My Email
// relay address belongs to exactly one Apple ID, so the match proves as much
// as a password-reset link sent to that address would. The old app signed
// Apple users in by email alone and never recorded the Apple ID, which left
// them unable to sign in here. An account already linked to a different
// Apple ID is refused rather than re-pointed.
func (h *AuthHandler) linkAppleByEmail(ctx context.Context, userID int64, appleSub string) error {
	var linkedElsewhere bool
	if err := h.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM social_providers WHERE user_id = $1 AND provider = 'apple')`, userID).Scan(&linkedElsewhere); err != nil {
		return fmt.Errorf("checking existing Apple link: %w", err)
	}
	if linkedElsewhere {
		return errSocialIdentityAlreadyOwned
	}
	if _, err := h.db.ExecContext(ctx,
		`INSERT INTO social_providers (user_id, provider, provider_id, created_at, updated_at)
		 SELECT $1::bigint, 'apple', $2::varchar, NOW(), NOW()
		 WHERE NOT EXISTS (SELECT 1 FROM social_providers WHERE provider = 'apple' AND provider_id = $2::varchar)`, userID, appleSub); err != nil {
		return fmt.Errorf("recording Apple link: %w", err)
	}
	slog.Info("social-login: linked Apple ID to existing account by verified email", "user_id", userID)
	return nil
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
			RefreshInterval: 24 * time.Hour,
			RefreshTimeout:  10 * time.Second,
			RefreshErrorHandler: func(err error) {
				slog.Error("apple JWKS refresh failed", "error", err)
			},
		})
	})
	return appleJWKS, appleJWKSErr
}

// verifyAppleIdentityToken validates an Apple Sign In identity token against
// Apple's JWKS and the expected audience (bundle ID). Returns (sub, email,
// emailVerified). Apple only provides email on first login; existing provider
// links continue to work without it.
func verifyAppleIdentityToken(idToken, audience string) (string, string, bool, error) {
	if audience == "" {
		return "", "", false, errors.New("Apple audience is not configured")
	}
	jwks, err := getAppleJWKS()
	if err != nil {
		return "", "", false, fmt.Errorf("loading Apple JWKS: %w", err)
	}

	token, err := jwt.Parse(idToken, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodRS256 {
			return nil, errors.New("unexpected Apple signing method")
		}
		return jwks.Keyfunc(token)
	})
	if err != nil {
		return "", "", false, fmt.Errorf("parsing Apple JWT: %w", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return "", "", false, errors.New("invalid Apple JWT")
	}

	if iss, _ := claims["iss"].(string); iss != "https://appleid.apple.com" {
		return "", "", false, fmt.Errorf("unexpected issuer: %s", iss)
	}
	switch aud := claims["aud"].(type) {
	case string:
		if aud != audience {
			return "", "", false, fmt.Errorf("unexpected audience: %s", aud)
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
			return "", "", false, errors.New("audience does not include bundle ID")
		}
	default:
		return "", "", false, errors.New("missing Apple audience")
	}

	sub, _ := claims["sub"].(string)
	if sub == "" {
		return "", "", false, errors.New("missing sub claim")
	}
	email, _ := claims["email"].(string)
	verified := claims["email_verified"] == true || claims["email_verified"] == "true"
	return sub, email, verified, nil
}

// --- Facebook access token verification ------------------------------------

// verifyFacebookAccessToken first uses debug_token to bind an access token to
// this configured Facebook application, then loads the user's provider data.
func verifyFacebookAccessToken(ctx context.Context, accessToken, appID, appSecret string, client *http.Client) (string, string, string, error) {
	if appID == "" || appSecret == "" {
		return "", "", "", errors.New("Facebook app credentials are not configured")
	}
	debugForm := url.Values{"input_token": {accessToken}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://graph.facebook.com/debug_token?"+debugForm.Encode(), nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+appID+"|"+appSecret)
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", errors.New("calling FB token debugger")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("FB token debugger %d", resp.StatusCode)
	}
	var debug struct {
		Data struct {
			AppID   string `json:"app_id"`
			IsValid bool   `json:"is_valid"`
			UserID  string `json:"user_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &debug); err != nil {
		return "", "", "", fmt.Errorf("decoding FB debug response: %w", err)
	}
	if !debug.Data.IsValid || debug.Data.AppID != appID || debug.Data.UserID == "" {
		return "", "", "", errors.New("Facebook token is not valid for this app")
	}
	meURL := "https://graph.facebook.com/me?" + url.Values{"fields": {"id,email,name"}}.Encode()
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, meURL, nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err = client.Do(req)
	if err != nil {
		return "", "", "", errors.New("calling FB graph API")
	}
	defer resp.Body.Close()
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("FB graph API %d", resp.StatusCode)
	}
	var data struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", "", "", fmt.Errorf("decoding FB response: %w", err)
	}
	if data.ID == "" || data.ID != debug.Data.UserID {
		return "", "", "", errors.New("FB returned no user id")
	}
	return data.ID, data.Email, data.Name, nil
}
