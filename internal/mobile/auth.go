package mobile

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	stdmail "net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/authjwt"
	"golang.org/x/crypto/bcrypt"
)

const maxBcryptPasswordBytes = 72

// PasswordResetMailer delivers a complete, opaque reset URL. The token is
// supplied separately for transports that need a template variable, but must
// never be logged or persisted by the mailer.
type PasswordResetMailer interface {
	SendPasswordReset(ctx context.Context, recipient, resetURL, token string) error
}

// AuthOptions contains optional integrations. With no reset mailer/password
// reset URL, recovery is explicitly unavailable rather than falsely claiming
// that an email has been sent. Facebook is disabled without app credentials.
type AuthOptions struct {
	ResetMailer       PasswordResetMailer
	ResetURL          string
	FacebookAppID     string
	FacebookAppSecret string
	HTTPClient        *http.Client
}

type AuthHandler struct {
	db                *sqlx.DB
	jwtSecret         []byte
	appleBundleID     string // expected audience for Apple identity tokens
	resetMailer       PasswordResetMailer
	resetURL          string
	facebookAppID     string
	facebookAppSecret string
	httpClient        *http.Client
	appleVerifier     func(string, string) (providerIdentity, error)
	facebookVerifier  func(context.Context, string) (providerIdentity, error)
}

func NewAuthHandler(db *sqlx.DB, jwtSecret, appleBundleID string, options ...AuthOptions) *AuthHandler {
	var option AuthOptions
	if len(options) > 0 {
		option = options[0]
	}
	client := option.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	h := &AuthHandler{db: db, jwtSecret: []byte(jwtSecret), appleBundleID: appleBundleID, resetMailer: option.ResetMailer, resetURL: option.ResetURL, facebookAppID: option.FacebookAppID, facebookAppSecret: option.FacebookAppSecret, httpClient: client}
	h.appleVerifier = func(token, audience string) (providerIdentity, error) {
		id, email, verified, err := verifyAppleIdentityToken(token, audience)
		return providerIdentity{ID: id, Email: email, EmailVerified: verified}, err
	}
	h.facebookVerifier = func(ctx context.Context, token string) (providerIdentity, error) {
		id, email, name, err := verifyFacebookAccessToken(ctx, token, h.facebookAppID, h.facebookAppSecret, h.httpClient)
		return providerIdentity{ID: id, Email: email, Name: name, EmailVerified: email != ""}, err
	}
	return h
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type registerRequest struct {
	Name                 string `json:"name"`
	Email                string `json:"email"`
	Password             string `json:"password"`
	PasswordConfirmation string `json:"password_confirmation"`
}

type authResponse struct {
	Token string   `json:"token"`
	User  AuthUser `json:"user"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	if req.Email == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email and password required"})
		return
	}
	if len(req.Password) > maxBcryptPasswordBytes {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be between 1 and 72 bytes"})
		return
	}

	var passwordHash string
	var user AuthUser
	err := h.db.QueryRowContext(r.Context(),
		`SELECT id, user_id, name, email, password, subscription_valid,
		 subscription_product_id, subscription_expiry, will_renew,
		 subscription_platform, superuser, token_version
		 FROM users WHERE email = $1`, req.Email).Scan(
		&user.ID, &user.UserID, &user.Name, &user.Email, &passwordHash,
		&user.SubscriptionValid, &user.SubscriptionProductID,
		&user.SubscriptionExpiry, &user.WillRenew, &user.SubscriptionPlatform,
		&user.Superuser, &user.TokenVersion)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	token, err := h.generateToken(&user)
	if err != nil {
		slog.Error("failed to generate token", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	writeJSON(w, http.StatusOK, authResponse{Token: token, User: user})
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	if req.Email == "" || req.Password == "" || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name, email and password required"})
		return
	}
	if !validPassword(req.Password) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be between 8 and 72 bytes"})
		return
	}

	if req.Password != req.PasswordConfirmation {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "passwords do not match"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	var user AuthUser
	err = h.db.QueryRowContext(r.Context(),
		`INSERT INTO users (user_id, name, email, password, created_at, updated_at)
		 VALUES (gen_random_uuid(), $1, $2, $3, NOW(), NOW())
		 RETURNING id, user_id, name, email, subscription_valid, subscription_product_id,
		 subscription_expiry, will_renew, subscription_platform, superuser`,
		req.Name, req.Email, string(hash)).Scan(
		&user.ID, &user.UserID, &user.Name, &user.Email,
		&user.SubscriptionValid, &user.SubscriptionProductID,
		&user.SubscriptionExpiry, &user.WillRenew, &user.SubscriptionPlatform,
		&user.Superuser)
	if err != nil {
		slog.Error("failed to create user", "error", err)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "email already taken"})
		return
	}

	token, err := h.generateToken(&user)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	writeJSON(w, http.StatusCreated, authResponse{Token: token, User: user})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if _, err := h.db.ExecContext(r.Context(), `UPDATE users SET token_version = token_version + 1, updated_at = NOW() WHERE id = $1`, user.ID); err != nil {
		slog.Error("failed to revoke session on logout", "error", err, "user_id", user.ID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *AuthHandler) UpdatePassword(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req struct {
		CurrentPassword      string `json:"current_password"`
		Password             string `json:"password"`
		PasswordConfirmation string `json:"password_confirmation"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	if req.Password != req.PasswordConfirmation {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "passwords do not match"})
		return
	}
	if !validPassword(req.Password) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be between 8 and 72 bytes"})
		return
	}

	// Verify current password
	var currentHash string
	if err := h.db.QueryRowContext(r.Context(), `SELECT password FROM users WHERE id = $1`, user.ID).Scan(&currentHash); err != nil {
		slog.Error("failed to load current password", "error", err, "user_id", user.ID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(req.CurrentPassword)); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "current password is incorrect"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("failed to hash password", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	tx, err := h.db.BeginTxx(r.Context(), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(r.Context(), `UPDATE users SET password = $1, token_version = token_version + 1, updated_at = NOW() WHERE id = $2`, string(hash), user.ID); err != nil {
		slog.Error("failed to update password", "error", err, "user_id", user.ID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE password_reset_tokens SET used_at = NOW() WHERE user_id = $1 AND used_at IS NULL`, user.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// UpdateProfile patches the current user's editable fields. Name can be
// changed freely. Email changes require the user's current password (because
// the email is the login identifier — we don't want a stolen session token
// to be enough to lock the real owner out). Email uniqueness is enforced.
func (h *AuthHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req struct {
		Name            *string `json:"name"`
		Email           *string `json:"email"`
		CurrentPassword string  `json:"current_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	emailChanging := req.Email != nil && !strings.EqualFold(strings.TrimSpace(*req.Email), user.Email)

	if emailChanging {
		if !validEmail(strings.TrimSpace(*req.Email)) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid email"})
			return
		}
		if req.CurrentPassword == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "current_password required to change email"})
			return
		}
		var hash string
		if err := h.db.QueryRowContext(r.Context(), `SELECT password FROM users WHERE id = $1`, user.ID).Scan(&hash); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.CurrentPassword)); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "current password is incorrect"})
			return
		}
	}

	// Build the update set dynamically — Postgres COALESCE keeps existing
	// values when a column is omitted.
	var (
		nameArg  any = nil
		emailArg any = nil
	)
	if req.Name != nil {
		nameArg = strings.TrimSpace(*req.Name)
	}
	if emailChanging {
		emailArg = strings.TrimSpace(*req.Email)
	}

	var updated AuthUser
	var err error
	query := `UPDATE users
		 SET name  = COALESCE($1, name),
		     email = COALESCE($2, email),
		     email_verified_at = CASE WHEN $2 IS NOT NULL AND $2 <> email THEN NULL ELSE email_verified_at END,
		     token_version = token_version + CASE WHEN $2 IS NOT NULL AND $2 <> email THEN 1 ELSE 0 END,
		     updated_at = NOW()
		 WHERE id = $3
		 RETURNING id, user_id, name, email, subscription_valid, subscription_product_id,
		           subscription_expiry, will_renew, subscription_platform, superuser`
	var tx *sqlx.Tx
	if emailChanging {
		tx, err = h.db.BeginTxx(r.Context(), nil)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		defer func() { _ = tx.Rollback() }()
	}
	queryRow := h.db.QueryRowContext
	if tx != nil {
		queryRow = tx.QueryRowContext
	}
	err = queryRow(r.Context(), query,
		nameArg, emailArg, user.ID).Scan(
		&updated.ID, &updated.UserID, &updated.Name, &updated.Email,
		&updated.SubscriptionValid, &updated.SubscriptionProductID,
		&updated.SubscriptionExpiry, &updated.WillRenew, &updated.SubscriptionPlatform,
		&updated.Superuser)
	if err != nil {
		// Most likely cause is the email unique constraint.
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "email already taken"})
			return
		}
		slog.Error("failed to update profile", "error", err, "user_id", user.ID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if tx != nil {
		if _, err := tx.ExecContext(r.Context(), `UPDATE password_reset_tokens SET used_at = NOW() WHERE user_id = $1 AND used_at IS NULL`, user.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		if err := tx.Commit(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
	}

	writeJSON(w, http.StatusOK, updated)
}

// DeleteAccount hard-deletes the current user and all rows owned by them.
// Required by Apple's App Store guideline 5.1.1(v). We run everything in a
// single transaction so a partial deletion can't leave dangling rows that
// reference a missing user.
//
// The request body must echo {"confirm": "DELETE"} and supply the current
// password — both as belt-and-braces against accidental or hijacked-session
// deletions.
func (h *AuthHandler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req struct {
		Confirm  string `json:"confirm"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Confirm != "DELETE" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": `confirm must be "DELETE"`})
		return
	}

	var hash string
	if err := h.db.QueryRowContext(r.Context(), `SELECT password FROM users WHERE id = $1`, user.ID).Scan(&hash); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "password is incorrect"})
		return
	}

	tx, err := h.db.BeginTxx(r.Context(), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	defer func() { _ = tx.Rollback() }()

	// Tables that reference users.id directly. Order matters only insofar as
	// the user row goes last; the rest have no inter-dependencies.
	cleanups := []string{
		`DELETE FROM responding         WHERE user_id = $1`,
		`DELETE FROM subscriptions      WHERE user_id = $1`,
		`DELETE FROM filters            WHERE user_id = $1`,
		`DELETE FROM alertzones         WHERE user_id = $1`,
		`DELETE FROM installations      WHERE user_id = $1`,
		`DELETE FROM iap_transactions   WHERE user_id = $1`,
		`DELETE FROM iap_subscription_ownership WHERE user_id = $1`,
		`DELETE FROM photos             WHERE user_id = $1`,
		`DELETE FROM sessions           WHERE user_id = $1`,
		`DELETE FROM social_providers   WHERE user_id = $1`,
		`DELETE FROM password_reset_tokens WHERE user_id = $1`,
		`DELETE FROM personal_access_tokens WHERE tokenable_type = 'App\Models\User' AND tokenable_id = $1`,
		`DELETE FROM notifications      WHERE notifiable_id = $1 AND notifiable_type = 'App\Models\User'`,
		// Don't delete systems the user owns — they may have shared data others
		// rely on. Disown instead.
		`UPDATE systems SET user_id = NULL, api_key = NULL, approved_at = NULL, updated_at = NOW() WHERE user_id = $1`,
	}
	for _, q := range cleanups {
		if _, err := tx.ExecContext(r.Context(), q, user.ID); err != nil {
			slog.Error("delete-account cleanup failed", "query", q, "error", err, "user_id", user.ID)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
	}

	if _, err := tx.ExecContext(r.Context(), `DELETE FROM users WHERE id = $1`, user.ID); err != nil {
		slog.Error("delete-account user delete failed", "error", err, "user_id", user.ID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	slog.Info("account deleted", "user_id", user.ID, "email", user.Email)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *AuthHandler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	if h.resetMailer == nil || h.resetURL == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "password recovery is not configured"})
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Email) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email required"})
		return
	}
	email := strings.TrimSpace(req.Email)
	var userID int64
	err := h.db.QueryRowContext(r.Context(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "If that email exists, a reset link has been sent."})
		return
	}
	if err != nil {
		slog.Error("failed to find reset user", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	tx, err := h.db.BeginTxx(r.Context(), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM password_reset_tokens WHERE user_id = $1`, userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO password_reset_tokens (user_id, token_hash, expires_at, created_at) VALUES ($1, $2, NOW() + INTERVAL '1 hour', NOW())`, userID, hash[:]); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	resetURL, err := resetURLWithToken(h.resetURL, token)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if err := h.resetMailer.SendPasswordReset(r.Context(), email, resetURL, token); err != nil {
		slog.Error("failed to deliver password reset", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to send password reset"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "If that email exists, a reset link has been sent."})
}

func (h *AuthHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token                string `json:"token"`
		Password             string `json:"password"`
		PasswordConfirmation string `json:"password_confirmation"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "token required"})
		return
	}
	if req.Password != req.PasswordConfirmation || !validPassword(req.Password) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid password"})
		return
	}
	hash := sha256.Sum256([]byte(req.Token))
	tx, err := h.db.BeginTxx(r.Context(), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	defer func() { _ = tx.Rollback() }()
	var userID int64
	if err := tx.QueryRowContext(r.Context(), `SELECT user_id FROM password_reset_tokens WHERE token_hash = $1 AND used_at IS NULL AND expires_at > NOW() FOR UPDATE`, hash[:]).Scan(&userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid or expired reset token"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE users SET password = $1, token_version = token_version + 1, updated_at = NOW() WHERE id = $2`, string(passwordHash), userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE password_reset_tokens SET used_at = NOW() WHERE user_id = $1 AND used_at IS NULL`, userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *AuthHandler) generateToken(user *AuthUser) (string, error) {
	return authjwt.Issue(h.jwtSecret, "rfspager-mobile", "mobile", user.ID, user.TokenVersion, 90*24*time.Hour, nil)
}

func validPassword(password string) bool {
	return len(password) >= 8 && len(password) <= maxBcryptPasswordBytes
}

func validEmail(email string) bool {
	parsed, err := stdmail.ParseAddress(email)
	return err == nil && parsed.Address == email
}

func resetURLWithToken(base, token string) (string, error) {
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("invalid password reset URL")
	}
	query := u.Query()
	query.Set("token", token)
	u.RawQuery = query.Encode()
	return u.String(), nil
}
