package mobile

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/jmoiron/sqlx"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	db             *sqlx.DB
	jwtSecret      []byte
	appleBundleID  string // expected audience for Apple identity tokens
}

func NewAuthHandler(db *sqlx.DB, jwtSecret, appleBundleID string) *AuthHandler {
	return &AuthHandler{db: db, jwtSecret: []byte(jwtSecret), appleBundleID: appleBundleID}
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

	var passwordHash string
	var user AuthUser
	err := h.db.QueryRowContext(r.Context(),
		`SELECT id, user_id, name, email, password, subscription_valid,
		 subscription_product_id, subscription_expiry, will_renew,
		 subscription_platform, superuser
		 FROM users WHERE email = $1`, req.Email).Scan(
		&user.ID, &user.UserID, &user.Name, &user.Email, &passwordHash,
		&user.SubscriptionValid, &user.SubscriptionProductID,
		&user.SubscriptionExpiry, &user.WillRenew, &user.SubscriptionPlatform,
		&user.Superuser)
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
	// JWT is stateless — client discards token. Nothing to do server-side.
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

	// Verify current password
	var currentHash string
	h.db.QueryRowContext(r.Context(), `SELECT password FROM users WHERE id = $1`, user.ID).Scan(&currentHash)
	if err := bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(req.CurrentPassword)); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "current password is incorrect"})
		return
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	h.db.ExecContext(r.Context(), `UPDATE users SET password = $1, updated_at = NOW() WHERE id = $2`, string(hash), user.ID)

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *AuthHandler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	// TODO: send reset email via SMTP
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "If that email exists, a reset link has been sent."})
}

func (h *AuthHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	// TODO: validate token and reset
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *AuthHandler) generateToken(user *AuthUser) (string, error) {
	claims := jwt.MapClaims{
		"sub":   user.ID,
		"email": user.Email,
		"exp":   time.Now().Add(90 * 24 * time.Hour).Unix(), // 90 days for mobile
		"iat":   time.Now().Unix(),
		"type":  "mobile",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(h.jwtSecret)
}
