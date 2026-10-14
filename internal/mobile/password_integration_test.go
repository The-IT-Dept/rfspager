package mobile

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

func passwordTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	raw := os.Getenv("RFSPAGER_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("RFSPAGER_TEST_DATABASE_URL is not set")
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.HasPrefix(strings.TrimPrefix(u.Path, "/"), "rfspager_test") {
		t.Fatalf("refusing to reset non-test database %q", raw)
	}
	db, err := sqlx.Connect("postgres", raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
		DROP SCHEMA public CASCADE;
		CREATE SCHEMA public;
		CREATE TABLE users (
			id BIGSERIAL PRIMARY KEY,
			user_id UUID NOT NULL DEFAULT gen_random_uuid(),
			name TEXT,
			email TEXT NOT NULL UNIQUE,
			password TEXT,
			password_changed_at TIMESTAMPTZ,
			email_verified_at TIMESTAMPTZ,
			token_version BIGINT NOT NULL DEFAULT 0,
			subscription_valid BOOLEAN NOT NULL DEFAULT false,
			subscription_product_id TEXT,
			subscription_expiry TEXT,
			will_renew BOOLEAN,
			subscription_platform TEXT,
			host_comp_until TIMESTAMPTZ,
			host_comp_since TIMESTAMPTZ,
			superuser BOOLEAN NOT NULL DEFAULT false,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW() - interval '30 days',
			updated_at TIMESTAMPTZ
		);
		CREATE TABLE social_providers (
			id BIGSERIAL PRIMARY KEY, user_id BIGINT NOT NULL, provider TEXT, provider_id TEXT,
			created_at TIMESTAMPTZ, updated_at TIMESTAMPTZ
		);
		CREATE TABLE password_reset_tokens (user_id BIGINT, used_at TIMESTAMPTZ);
	`); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return db
}

// seedUser creates an account; socialAfter is when a provider was linked
// relative to account creation ("" for none).
func seedUser(t *testing.T, db *sqlx.DB, email, password, socialAfter string) *AuthUser {
	t.Helper()
	var stored any
	if password != "" {
		stored = password
		if password != "!" {
			h, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
			stored = string(h)
		}
	}
	var u AuthUser
	if err := db.QueryRowx(`INSERT INTO users (email, password) VALUES ($1, $2)
		RETURNING id, user_id, email, token_version`, email, stored).Scan(&u.ID, &u.UserID, &u.Email, &u.TokenVersion); err != nil {
		t.Fatal(err)
	}
	if socialAfter != "" {
		if _, err := db.Exec(`INSERT INTO social_providers (user_id, provider, provider_id, created_at)
			SELECT id, 'apple', 'p-'||id, created_at + $2::interval FROM users WHERE id = $1`, u.ID, socialAfter); err != nil {
			t.Fatal(err)
		}
	}
	return &u
}

func postAs(h http.HandlerFunc, u *AuthUser, body string) (int, map[string]any) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), UserContextKey, u))
	rr := httptest.NewRecorder()
	h(rr, req)
	var out map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	return rr.Code, out
}

func TestHasPasswordRule(t *testing.T) {
	db := passwordTestDB(t)
	h := NewAuthHandler(db, "test-secret-test-secret-test-secret", "app.example")
	cases := []struct {
		name, password, socialAfter string
		known                       bool
	}{
		{"email signup", "hunter2hunter2", "", true},
		{"laravel social signup (random hash)", "random-unknown", "1 second", false},
		{"go social signup ('!')", "!", "0 seconds", false},
		{"no password at all", "", "", false},
		{"email account that linked social later", "hunter2hunter2", "2 days", true},
	}
	for i, tc := range cases {
		u := seedUser(t, db, "u"+string(rune('a'+i))+"@example.invalid", tc.password, tc.socialAfter)
		_, known, err := h.passwordState(context.Background(), u.ID)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if known != tc.known {
			t.Fatalf("%s: known=%v want %v", tc.name, known, tc.known)
		}
	}
}

func TestUpdatePasswordRequiresCurrentOnlyWhenKnown(t *testing.T) {
	db := passwordTestDB(t)
	h := NewAuthHandler(db, "test-secret-test-secret-test-secret", "app.example")

	email := seedUser(t, db, "email@example.invalid", "old-password-1", "")
	if code, _ := postAs(h.UpdatePassword, email, `{"password":"new-password-1","password_confirmation":"new-password-1"}`); code != http.StatusBadRequest {
		t.Fatalf("missing current password: %d", code)
	}
	if code, _ := postAs(h.UpdatePassword, email, `{"current_password":"wrong","password":"new-password-1","password_confirmation":"new-password-1"}`); code != http.StatusUnprocessableEntity {
		t.Fatalf("wrong current password: %d", code)
	}
	code, out := postAs(h.UpdatePassword, email, `{"current_password":"old-password-1","password":"new-password-1","password_confirmation":"new-password-1"}`)
	if code != http.StatusOK || out["token"] == nil || out["token"] == "" {
		t.Fatalf("change with current password: %d %v", code, out)
	}

	social := seedUser(t, db, "social@example.invalid", "random-unknown", "1 second")
	code, out = postAs(h.UpdatePassword, social, `{"password":"chosen-pass-1","password_confirmation":"chosen-pass-1"}`)
	if code != http.StatusOK || out["token"] == nil {
		t.Fatalf("social account setting a password: %d %v", code, out)
	}
	var stamped bool
	_ = db.Get(&stamped, `SELECT password_changed_at IS NOT NULL FROM users WHERE id = $1`, social.ID)
	if !stamped {
		t.Fatal("password_changed_at not recorded")
	}
	// Having set one, they now need it like anyone else.
	social.TokenVersion++
	if code, _ := postAs(h.UpdatePassword, social, `{"password":"another-pass-1","password_confirmation":"another-pass-1"}`); code != http.StatusBadRequest {
		t.Fatalf("after setting a password, current is required: %d", code)
	}
}

func TestUpdateEmailWithoutKnownPasswordReturnsToken(t *testing.T) {
	db := passwordTestDB(t)
	h := NewAuthHandler(db, "test-secret-test-secret-test-secret", "app.example")
	social := seedUser(t, db, "relay@privaterelay.appleid.com", "!", "0 seconds")
	code, out := postAs(h.UpdateProfile, social, `{"email":"me@example.invalid"}`)
	if code != http.StatusOK || out["email"] != "me@example.invalid" || out["token"] == nil {
		t.Fatalf("email change: %d %v", code, out)
	}
	email := seedUser(t, db, "known@example.invalid", "old-password-1", "")
	if code, _ := postAs(h.UpdateProfile, email, `{"email":"known2@example.invalid"}`); code != http.StatusBadRequest {
		t.Fatalf("email change without the known password: %d", code)
	}
	// A name-only change returns no token.
	if code, out := postAs(h.UpdateProfile, email, `{"name":"Nick"}`); code != http.StatusOK || out["token"] != nil {
		t.Fatalf("name change: %d %v", code, out)
	}
}
