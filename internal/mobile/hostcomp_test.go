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
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/the-it-dept/rfspager/internal/db"
	"github.com/the-it-dept/rfspager/internal/hostcomp"
)

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

func TestWithEffectiveAccess(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	future := now.Add(7 * 24 * time.Hour)
	past := now.Add(-time.Minute)

	// Comp only: reported as the host product, with no platform.
	u := AuthUser{SubscriptionProductID: strPtr("io.devlan.rfspager.monthly"), SubscriptionPlatform: strPtr("apple"),
		SubscriptionExpiry: strPtr("2026-01-01T00:00:00Z"), WillRenew: boolPtr(false), HostCompUntil: &future}
	u.withEffectiveAccess(now)
	if !u.SubscriptionValid || *u.SubscriptionProductID != hostcomp.ProductID || u.SubscriptionPlatform != nil || u.WillRenew != nil ||
		*u.SubscriptionExpiry != "2026-10-21T10:00:00Z" {
		t.Fatalf("comp only = %+v", u)
	}

	// A valid store subscription is reported as stored, comp or not.
	u = AuthUser{SubscriptionValid: true, SubscriptionProductID: strPtr("io.devlan.rfspager.yearly"), SubscriptionPlatform: strPtr("google"),
		SubscriptionExpiry: strPtr("2027-01-01T00:00:00Z"), WillRenew: boolPtr(true), HostCompUntil: &future}
	u.withEffectiveAccess(now)
	if !u.SubscriptionValid || *u.SubscriptionProductID != "io.devlan.rfspager.yearly" || *u.SubscriptionPlatform != "google" || !*u.WillRenew {
		t.Fatalf("store + comp = %+v", u)
	}

	// A gift is a store-slot entitlement: unchanged.
	u = AuthUser{SubscriptionValid: true, SubscriptionProductID: strPtr("io.devlan.rfspager.gifted")}
	u.withEffectiveAccess(now)
	if !u.SubscriptionValid || *u.SubscriptionProductID != "io.devlan.rfspager.gifted" {
		t.Fatalf("gift = %+v", u)
	}

	// Expired comp, or none: no access, stored fields untouched.
	for _, until := range []*time.Time{&past, &now, nil} {
		u = AuthUser{SubscriptionProductID: strPtr("io.devlan.rfspager.monthly"), SubscriptionPlatform: strPtr("apple"), HostCompUntil: until}
		u.withEffectiveAccess(now)
		if u.SubscriptionValid || *u.SubscriptionProductID != "io.devlan.rfspager.monthly" || *u.SubscriptionPlatform != "apple" {
			t.Fatalf("until %v = %+v", until, u)
		}
	}
}

func hostCompMobileDB(t *testing.T) *sqlx.DB {
	t.Helper()
	rawURL := os.Getenv("RFSPAGER_TEST_DATABASE_URL")
	if rawURL == "" {
		t.Skip("RFSPAGER_TEST_DATABASE_URL is not set")
	}
	u, err := url.Parse(rawURL)
	if err != nil || !strings.HasPrefix(strings.TrimPrefix(u.Path, "/"), "rfspager_test") {
		t.Fatalf("RFSPAGER_TEST_DATABASE_URL must name a rfspager_test* database")
	}
	d, err := db.New(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := context.Background()
	if _, err := d.Conn().ExecContext(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return d.Conn()
}

// /me and the middleware's user against the real schema: a comped user with
// a stale, lapsed store subscription reads as Pro on the host product.
func TestMeReportsHostComp(t *testing.T) {
	conn := hostCompMobileDB(t)
	const secret = "local-test-secret"
	auth := NewAuthHandler(conn, secret, "")
	h := NewHandlers(conn)

	seed := func(email, set string, args ...any) string {
		t.Helper()
		var user AuthUser
		if err := conn.Get(&user, `INSERT INTO users (user_id, name, email, password) VALUES (gen_random_uuid(), 'Host', $1, '!')
			RETURNING `+authUserColumns, email); err != nil {
			t.Fatal(err)
		}
		if set != "" {
			if _, err := conn.Exec(`UPDATE users SET `+set+` WHERE id = $1`, append([]any{user.ID}, args...)...); err != nil {
				t.Fatal(err)
			}
		}
		token, err := auth.generateToken(&user)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	me := func(token string) (map[string]any, *AuthUser) {
		t.Helper()
		var ctxUser *AuthUser
		handler := JWTUserAuth(conn, secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctxUser = GetUser(r.Context())
			h.Me(w, r)
		}))
		req := httptest.NewRequest(http.MethodGet, "/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("/me: %d %s", rec.Code, rec.Body)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		t.Logf("/me: %s", rec.Body)
		return body, ctxUser
	}

	until := time.Now().UTC().Add(7 * 24 * time.Hour).Truncate(time.Second)
	comped := seed("comped@example.invalid",
		`subscription_valid = false, subscription_product_id = 'io.devlan.rfspager.monthly', subscription_platform = 'apple',
		 subscription_expiry = '2026-01-01 00:00:00', will_renew = false, host_comp_until = $2, host_comp_since = NOW()`, until)
	body, ctxUser := me(comped)
	if body["subscription_valid"] != true || body["subscription_product_id"] != hostcomp.ProductID || body["subscription_platform"] != nil ||
		body["will_renew"] != nil || body["subscription_expiry"] != until.Format(time.RFC3339Nano) || body["host_comp_until"] == nil {
		t.Fatalf("comped /me = %v", body)
	}
	if !ctxUser.SubscriptionValid {
		t.Fatal("middleware user is not Pro, so Pro features stay gated")
	}
	var stored struct {
		Valid   bool   `db:"subscription_valid"`
		Product string `db:"subscription_product_id"`
	}
	if err := conn.Get(&stored, `SELECT subscription_valid, subscription_product_id FROM users WHERE email = 'comped@example.invalid'`); err != nil {
		t.Fatal(err)
	}
	if stored.Valid || stored.Product != "io.devlan.rfspager.monthly" {
		t.Fatalf("reporting wrote the store slot: %+v", stored)
	}

	paying := seed("paying@example.invalid",
		`subscription_valid = true, subscription_product_id = 'io.devlan.rfspager.yearly', subscription_platform = 'google',
		 subscription_expiry = '2027-03-01 12:00:00', will_renew = true, host_comp_until = $2`, until)
	body, _ = me(paying)
	if body["subscription_valid"] != true || body["subscription_product_id"] != "io.devlan.rfspager.yearly" || body["subscription_platform"] != "google" ||
		body["will_renew"] != true || body["subscription_expiry"] != "2027-03-01T12:00:00Z" || body["host_comp_until"] == nil {
		t.Fatalf("paying + comped /me = %v", body)
	}

	lapsed := seed("lapsed@example.invalid", `host_comp_until = NOW() - INTERVAL '1 hour'`)
	body, ctxUser = me(lapsed)
	if body["subscription_valid"] != false || body["subscription_product_id"] != nil || body["host_comp_until"] == nil || ctxUser.SubscriptionValid {
		t.Fatalf("lapsed /me = %v", body)
	}

	free := seed("free@example.invalid", "")
	if body, _ = me(free); body["subscription_valid"] != false || body["host_comp_until"] != nil {
		t.Fatalf("free /me = %v", body)
	}
}
