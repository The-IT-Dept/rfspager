package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
)

func TestSecurityAdministratorManagementRequiresSuperadmin(t *testing.T) {
	h := NewAuthHandler(nil, "test-secret")
	called := false
	handler := h.RequireSuperadmin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	req := httptest.NewRequest(http.MethodPost, "/admin-users", nil)
	req = req.WithContext(context.WithValue(req.Context(), AdminUserContextKey, &AdminUser{Role: "admin"}))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if called || rr.Code != http.StatusForbidden {
		t.Fatalf("admin account management allowed role=admin: called=%v status=%d", called, rr.Code)
	}
}

func TestSecuritySuperadminCanManageAdministrators(t *testing.T) {
	h := NewAuthHandler(nil, "test-secret")
	handler := h.RequireSuperadmin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	req := httptest.NewRequest(http.MethodPost, "/admin-users", nil)
	req = req.WithContext(context.WithValue(req.Context(), AdminUserContextKey, &AdminUser{Role: "superadmin"}))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestSecuritySuperadminCannotDeleteOwnAdministratorAccount(t *testing.T) {
	h := NewAuthHandler(nil, "test-secret")
	called := false
	handler := h.PreventSelfAdminDelete(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	req := httptest.NewRequest(http.MethodDelete, "/admin-users/7", nil)
	ctx := context.WithValue(req.Context(), AdminUserContextKey, &AdminUser{ID: 7, Role: "superadmin"})
	// chi records route parameters in the context when dispatching; emulate the
	// route here so the middleware is tested at its HTTP boundary.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "7")
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if called || rr.Code != http.StatusConflict {
		t.Fatalf("self delete allowed: called=%v status=%d", called, rr.Code)
	}
}

func TestSecurityStoreRejectsMaliciousWritableField(t *testing.T) {
	store := NewStore(nil)
	malicious := `name" = (SELECT password_hash FROM admin_users WHERE role='superadmin' LIMIT 1), "email`
	if _, err := store.CreateAdminUser(context.Background(), map[string]any{"email": "a@example.invalid", "name": "A", "password": "password", malicious: "x"}); err == nil {
		t.Fatal("malicious admin field was accepted")
	}
	if err := store.UpdateUser(context.Background(), 7, map[string]any{malicious: "x"}); err == nil {
		t.Fatal("malicious user field was accepted")
	}
}

func TestSecurityAdminLogoutAdvancesTokenVersion(t *testing.T) {
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	mock.ExpectExec(`UPDATE admin_users SET token_version = token_version \+ 1`).WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	h := NewAuthHandler(NewStore(sqlx.NewDb(conn, "sqlmock")), "test-secret")
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req = req.WithContext(context.WithValue(req.Context(), AdminUserContextKey, &AdminUser{ID: 7}))
	rr := httptest.NewRecorder()
	h.Logout(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSecurityAdminPasswordChangeRevokesSessionsAndResetTokens(t *testing.T) {
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE users SET .*token_version = token_version \+ 1`).WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), int64(9)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE password_reset_tokens SET used_at`).WithArgs(int64(9)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	store := NewStore(sqlx.NewDb(conn, "sqlmock"))
	if err := store.UpdateUser(context.Background(), 9, map[string]any{"password": "a-valid-password"}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSecurityMapRowsRedactAuthenticationSecrets(t *testing.T) {
	row := map[string]any{"email": "member@example.invalid", "password": "hash", "password_hash": "admin-hash", "remember_token": "remember", "token_version": int64(3), "api_key": "receiver-key"}
	cleanMapScan(row)
	for _, field := range []string{"password", "password_hash", "remember_token", "token_version"} {
		if _, present := row[field]; present {
			t.Fatalf("exposed %s", field)
		}
	}
	if row["api_key"] != "receiver-key" {
		t.Fatal("receiver api key was incorrectly redacted")
	}
}
