package mobile

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/admin"
)

// securityDriver is deliberately small: the security boundary is the real
// token generator and middleware, with a controlled database response.
type securityDriver struct {
	query func(string, []driver.NamedValue) (driver.Rows, error)
	exec  func(string, []driver.NamedValue) (driver.Result, error)
	begin func() (driver.Tx, error)
}

func TestSecurityAdminTokenCannotAuthenticateAsMobile(t *testing.T) {
	db := securityDB(t, &securityDriver{query: func(q string, a []driver.NamedValue) (driver.Rows, error) {
		if !strings.Contains(q, "FROM users WHERE id") || a[0].Value != int64(7) {
			return nil, fmt.Errorf("unexpected query %q", q)
		}
		return &securityRows{cols: []string{"id", "user_id", "name", "email", "subscription_valid", "subscription_product_id", "subscription_expiry", "will_renew", "subscription_platform", "superuser"}, values: [][]driver.Value{{int64(7), "member-id", "Member", "member@example.invalid", false, nil, nil, nil, nil, false}}}, nil
	}})
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": "rfspager", "aud": "rfspager-admin", "typ": "admin", "sub": int64(7), "ver": int64(0), "exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, err := token.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}

	called := false
	handler := JWTUserAuth(db, "test-secret")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called || rr.Code != http.StatusUnauthorized {
		t.Fatalf("admin token reached mobile handler: called=%v status=%d", called, rr.Code)
	}
}

func (d *securityDriver) Open(string) (driver.Conn, error) { return &securityConn{d}, nil }

type securityConn struct{ d *securityDriver }

func (*securityConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("unexpected prepare")
}
func (*securityConn) Close() error { return nil }
func (c *securityConn) Begin() (driver.Tx, error) {
	if c.d.begin == nil {
		return nil, fmt.Errorf("unexpected begin")
	}
	return c.d.begin()
}
func (c *securityConn) QueryContext(_ context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	return c.d.query(q, a)
}
func (c *securityConn) ExecContext(_ context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	if c.d.exec == nil {
		return nil, fmt.Errorf("unexpected exec %q", q)
	}
	return c.d.exec(q, a)
}

type securityRows struct {
	cols   []string
	values [][]driver.Value
}

func (r *securityRows) Columns() []string { return r.cols }
func (*securityRows) Close() error        { return nil }
func (r *securityRows) Next(v []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(v, r.values[0])
	r.values = r.values[1:]
	return nil
}
func securityDB(t *testing.T, d *securityDriver) *sqlx.DB {
	t.Helper()
	sql.Register(t.Name(), d)
	db, err := sql.Open(t.Name(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return sqlx.NewDb(db, t.Name())
}

func TestSecurityMobileTokenCannotAuthenticateAsAdmin(t *testing.T) {
	now := time.Now()
	db := securityDB(t, &securityDriver{query: func(q string, a []driver.NamedValue) (driver.Rows, error) {
		if !strings.Contains(q, "FROM admin_users WHERE id") || a[0].Value != int64(7) {
			return nil, fmt.Errorf("unexpected query %q", q)
		}
		return &securityRows{cols: []string{"id", "email", "password_hash", "name", "role", "active", "last_login_at", "created_at", "updated_at"}, values: [][]driver.Value{{int64(7), "admin@example.invalid", "hash", "Admin", "superadmin", true, nil, now, now}}}, nil
	}})
	token, err := NewAuthHandler(db, "test-secret", "app.example").generateToken(&AuthUser{ID: 7, Email: "member@example.invalid"})
	if err != nil {
		t.Fatal(err)
	}

	called := false
	handler := admin.NewAuthHandler(admin.NewStore(db), "test-secret").JWTAuth(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called || rr.Code != http.StatusUnauthorized {
		t.Fatalf("mobile token reached admin handler: called=%v status=%d", called, rr.Code)
	}
}

func TestSecurityMalformedSubjectIsRejectedWithoutPanic(t *testing.T) {
	db := securityDB(t, &securityDriver{query: func(q string, a []driver.NamedValue) (driver.Rows, error) {
		return nil, fmt.Errorf("database must not be queried for malformed claims")
	}})
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": "rfspager", "aud": "rfspager-mobile", "typ": "mobile", "sub": "7", "ver": int64(0), "exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, err := token.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	handler := JWTUserAuth(db, "test-secret")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("handler called") }))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rr.Code)
	}
}

func TestSecuritySocialLoginCannotLinkEmailMatchedAccount(t *testing.T) {
	db := securityDB(t, &securityDriver{query: func(q string, a []driver.NamedValue) (driver.Rows, error) {
		switch {
		case strings.Contains(q, "FROM social_providers"):
			return &securityRows{cols: []string{"user_id"}}, nil
		case strings.Contains(q, "SELECT id FROM users WHERE lower(email) = lower($1)"):
			if a[0].Value != "victim@example.invalid" {
				return nil, fmt.Errorf("email=%v", a[0].Value)
			}
			return &securityRows{cols: []string{"id"}, values: [][]driver.Value{{int64(42)}}}, nil
		default:
			return nil, fmt.Errorf("unexpected query %q", q)
		}
	}})
	h := NewAuthHandler(db, "test-secret", "app.example")
	h.facebookVerifier = func(context.Context, string) (providerIdentity, error) {
		return providerIdentity{ID: "attacker-provider-id", Email: "victim@example.invalid", Name: "Attacker", EmailVerified: true}, nil
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/social-login", strings.NewReader(`{"provider":"facebook","token":"attacker-token","email":"victim@example.invalid"}`))
	rr := httptest.NewRecorder()
	h.SocialLogin(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSecuritySocialLoginRequiresVerifiedProviderEmailForNewAccount(t *testing.T) {
	db := securityDB(t, &securityDriver{query: func(q string, a []driver.NamedValue) (driver.Rows, error) {
		if !strings.Contains(q, "FROM social_providers") {
			return nil, fmt.Errorf("unexpected query %q", q)
		}
		return &securityRows{cols: []string{"user_id"}}, nil
	}})
	h := NewAuthHandler(db, "test-secret", "app.example")
	h.appleVerifier = func(string, string) (providerIdentity, error) { return providerIdentity{ID: "apple-sub"}, nil }
	req := httptest.NewRequest(http.MethodPost, "/auth/social-login", strings.NewReader(`{"provider":"apple","token":"apple-token","email":"victim@example.invalid"}`))
	rr := httptest.NewRecorder()
	h.SocialLogin(rr, req)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

type securityTransport func(*http.Request) (*http.Response, error)

func (f securityTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSecurityFacebookDebugRejectsWrongApplicationBeforeProfileLookup(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: securityTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/debug_token" {
			return nil, fmt.Errorf("profile lookup for wrong app")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":{"is_valid":true,"app_id":"other-app","user_id":"attacker"}}`))}, nil
	})}
	_, _, _, err := verifyFacebookAccessToken(context.Background(), "provider-token", "expected-app", "app-secret", client)
	if err == nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestSecurityFacebookTransportFailureDoesNotExposeCredentials(t *testing.T) {
	client := &http.Client{Transport: securityTransport(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("network unavailable")
	})}
	_, _, _, err := verifyFacebookAccessToken(context.Background(), "user-token-secret", "app-id-secret", "app-secret-secret", client)
	if err == nil {
		t.Fatal("expected provider failure")
	}
	message := err.Error()
	for _, secret := range []string{"user-token-secret", "app-id-secret", "app-secret-secret"} {
		if strings.Contains(message, secret) {
			t.Fatalf("provider error leaked secret %q: %s", secret, message)
		}
	}
}

func TestSecurityForgotPasswordIsUnavailableWithoutDeliveryConfiguration(t *testing.T) {
	h := NewAuthHandler(nil, "test-secret", "app.example")
	req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", strings.NewReader(`{"email":"member@example.invalid"}`))
	rr := httptest.NewRecorder()
	h.ForgotPassword(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", rr.Code)
	}
}

type securityTx struct{ committed bool }

func (tx *securityTx) Commit() error { tx.committed = true; return nil }
func (*securityTx) Rollback() error  { return nil }

type capturedResetMail struct{ recipient, name, resetURL, token string }

func (m *capturedResetMail) SendPasswordReset(_ context.Context, recipient, name, resetURL, token string) error {
	m.recipient, m.name, m.resetURL, m.token = recipient, name, resetURL, token
	return nil
}

func TestSecurityForgotPasswordStoresOnlyHashAndSendsOpaqueToken(t *testing.T) {
	tx := &securityTx{}
	var storedHash []byte
	db := securityDB(t, &securityDriver{
		query: func(q string, a []driver.NamedValue) (driver.Rows, error) {
			if !strings.Contains(q, "FROM users WHERE lower(email) = lower($1) ORDER BY (email = $1) DESC") || a[0].Value != "Member@Example.invalid" {
				return nil, fmt.Errorf("unexpected query %q", q)
			}
			return &securityRows{cols: []string{"id", "name", "email"}, values: [][]driver.Value{{int64(4), "Member Name", "member@example.invalid"}}}, nil
		},
		exec: func(q string, a []driver.NamedValue) (driver.Result, error) {
			switch {
			case strings.Contains(q, "DELETE FROM password_reset_tokens"):
				return driver.RowsAffected(1), nil
			case strings.Contains(q, "INSERT INTO password_reset_tokens"):
				storedHash, _ = a[1].Value.([]byte)
				return driver.RowsAffected(1), nil
			default:
				return nil, fmt.Errorf("unexpected exec %q", q)
			}
		},
		begin: func() (driver.Tx, error) { return tx, nil },
	})
	mail := &capturedResetMail{}
	h := NewAuthHandler(db, "test-secret", "app.example", AuthOptions{ResetMailer: mail, ResetURL: "https://app.example/reset-password"})
	req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", strings.NewReader(`{"email":"Member@Example.invalid"}`))
	rr := httptest.NewRecorder()
	h.ForgotPassword(rr, req)
	if rr.Code != http.StatusOK || !tx.committed {
		t.Fatalf("status=%d committed=%v", rr.Code, tx.committed)
	}
	if mail.recipient != "member@example.invalid" || mail.name != "Member Name" || mail.token == "" || !strings.Contains(mail.resetURL, "token=") {
		t.Fatalf("mail=%+v", mail)
	}
	wantHash := sha256.Sum256([]byte(mail.token))
	if string(storedHash) != string(wantHash[:]) {
		t.Fatal("stored reset secret was not the token hash")
	}
}

func TestSecurityResetPasswordConsumesTokenAndRevokesSessions(t *testing.T) {
	tx := &securityTx{}
	resetToken := "opaque-reset-token"
	wantHash := sha256.Sum256([]byte(resetToken))
	updatedPassword := ""
	used := false
	db := securityDB(t, &securityDriver{
		query: func(q string, a []driver.NamedValue) (driver.Rows, error) {
			if !strings.Contains(q, "FROM password_reset_tokens") || string(a[0].Value.([]byte)) != string(wantHash[:]) {
				return nil, fmt.Errorf("unexpected reset lookup")
			}
			return &securityRows{cols: []string{"user_id"}, values: [][]driver.Value{{int64(4)}}}, nil
		},
		exec: func(q string, a []driver.NamedValue) (driver.Result, error) {
			switch {
			case strings.Contains(q, "UPDATE users SET password"):
				updatedPassword, _ = a[0].Value.(string)
				if a[1].Value != int64(4) {
					return nil, fmt.Errorf("wrong user")
				}
				return driver.RowsAffected(1), nil
			case strings.Contains(q, "UPDATE password_reset_tokens SET used_at"):
				used = true
				return driver.RowsAffected(1), nil
			default:
				return nil, fmt.Errorf("unexpected exec %q", q)
			}
		},
		begin: func() (driver.Tx, error) { return tx, nil },
	})
	h := NewAuthHandler(db, "test-secret", "app.example")
	req := httptest.NewRequest(http.MethodPost, "/auth/reset-password", strings.NewReader(`{"token":"opaque-reset-token","password":"new-password","password_confirmation":"new-password"}`))
	rr := httptest.NewRecorder()
	h.ResetPassword(rr, req)
	if rr.Code != http.StatusOK || !tx.committed || !used {
		t.Fatalf("status=%d committed=%v used=%v", rr.Code, tx.committed, used)
	}
	if updatedPassword == "" {
		t.Fatal("password was not updated")
	}
}
