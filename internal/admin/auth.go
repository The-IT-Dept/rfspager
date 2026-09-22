package admin

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/authjwt"
	"golang.org/x/crypto/bcrypt"
)

type AdminUser struct {
	ID           int64      `db:"id" json:"id"`
	Email        string     `db:"email" json:"email"`
	PasswordHash string     `db:"password_hash" json:"-"`
	Name         string     `db:"name" json:"name"`
	Role         string     `db:"role" json:"role"`
	Active       bool       `db:"active" json:"active"`
	LastLoginAt  *time.Time `db:"last_login_at" json:"last_login_at"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at" json:"updated_at"`
	TokenVersion int64      `db:"token_version" json:"-"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token string    `json:"token"`
	User  AdminUser `json:"user"`
}

type contextKey string

const AdminUserContextKey contextKey = "admin_user"

type AuthHandler struct {
	store     *Store
	jwtSecret []byte
}

func NewAuthHandler(store *Store, jwtSecret string) *AuthHandler {
	return &AuthHandler{store: store, jwtSecret: []byte(jwtSecret)}
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	if req.Email == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email and password required"})
		return
	}

	user, err := h.store.GetAdminByEmail(r.Context(), req.Email)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	if !user.Active {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "account disabled"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	if err := h.store.UpdateAdminLastLogin(r.Context(), user.ID); err != nil {
		slog.Error("failed to update admin login", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	token, err := h.generateToken(user)
	if err != nil {
		slog.Error("failed to generate token", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	writeJSON(w, http.StatusOK, LoginResponse{Token: token, User: *user})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	user := GetAdminUser(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	writeJSON(w, http.StatusOK, user)
}

// Logout invalidates the current admin token and every other session for that
// account by advancing its persisted session version.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	user := GetAdminUser(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if err := h.store.RevokeAdminSessions(r.Context(), user.ID); err != nil {
		slog.Error("failed to revoke admin sessions", "error", err, "admin_id", user.ID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *AuthHandler) generateToken(user *AdminUser) (string, error) {
	return authjwt.Issue(h.jwtSecret, "rfspager-admin", "admin", user.ID, user.TokenVersion, 24*time.Hour, nil)
}

// NewJWTAuthMiddleware authenticates only active, non-revoked admin sessions.
// It is exported for admin streams mounted outside Router.
func NewJWTAuthMiddleware(db *sqlx.DB, jwtSecret string) func(http.Handler) http.Handler {
	return NewAuthHandler(NewStore(db), jwtSecret).JWTAuthHeaderOrQuery
}

// JWTAuth middleware validates JWT tokens for admin routes.
func (h *AuthHandler) JWTAuth(next http.Handler) http.Handler {
	return h.jwtAuth(next, false)
}

// JWTAuthHeaderOrQuery is the same as JWTAuth but also accepts the token
// in a `token` query parameter. Browser EventSource can't set custom
// headers, so SSE endpoints use this variant.
func (h *AuthHandler) JWTAuthHeaderOrQuery(next http.Handler) http.Handler {
	return h.jwtAuth(next, true)
}

func (h *AuthHandler) jwtAuth(next http.Handler, allowQueryToken bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		var tokenStr string
		if authHeader != "" {
			tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
			if tokenStr == authHeader {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid authorization header"})
				return
			}
		} else if allowQueryToken {
			tokenStr = r.URL.Query().Get("token")
		}
		if tokenStr == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		claims, err := authjwt.Validate(h.jwtSecret, tokenStr, "rfspager-admin", "admin")
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
			return
		}
		user, err := h.store.GetAdminByID(r.Context(), claims.Subject)
		if err != nil || !user.Active || user.TokenVersion != claims.Version {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		ctx := context.WithValue(r.Context(), AdminUserContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireSuperadmin protects administrator account management.
func (h *AuthHandler) RequireSuperadmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetAdminUser(r.Context())
		if user == nil || user.Role != "superadmin" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "superadmin required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// PreventSelfAdminDelete leaves a superadmin able to recover an account if an
// operator makes a mistake in the administrator management UI.
func (h *AuthHandler) PreventSelfAdminDelete(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetAdminUser(r.Context())
		targetID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if user != nil && err == nil && targetID == user.ID {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "cannot delete the current administrator"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func GetAdminUser(ctx context.Context) *AdminUser {
	u, _ := ctx.Value(AdminUserContextKey).(*AdminUser)
	return u
}
