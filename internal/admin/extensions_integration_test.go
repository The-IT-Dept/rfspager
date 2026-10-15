package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/the-it-dept/rfspager/internal/iap"
)

type fakeApple struct {
	calls  []int
	failOn int // 1-based call number that fails; 0 = none
	expiry time.Time
}

func (f *fakeApple) ExtendRenewalDate(environment, otx string, days, reason int, requestID string) (*iap.AppleExtendResult, error) {
	f.calls = append(f.calls, days)
	if len(f.calls) == f.failOn {
		return nil, errors.New("Apple API returned 400: SubscriptionMaxExtensionError")
	}
	f.expiry = f.expiry.AddDate(0, 0, days)
	return &iap.AppleExtendResult{EffectiveDate: f.expiry.UnixMilli(), OriginalTransactionID: otx, Success: true}, nil
}

type fakeGoogle struct {
	days   []int
	expiry time.Time
}

func (f *fakeGoogle) DeferSubscription(ctx context.Context, token string, days int) (time.Time, error) {
	f.days = append(f.days, days)
	return f.expiry.AddDate(0, 0, days), nil
}

func grantRequest(t *testing.T, h *extensionHandlers, admin *AdminUser, userID int64, body map[string]any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.FormatInt(userID, 10))
	ctx := context.WithValue(context.WithValue(req.Context(), chi.RouteCtxKey, rctx), AdminUserContextKey, admin)
	rec := httptest.NewRecorder()
	h.Grant(rec, req.WithContext(ctx))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func listExtensions(t *testing.T, h *extensionHandlers, userID int64) extensionsResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.FormatInt(userID, 10))
	rec := httptest.NewRecorder()
	h.List(rec, req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx)))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	var out extensionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// "Give free time" follows the account: Apple renewal extensions (split into
// 90-day calls, two a year), a Google deferral, or a gift for everyone else.
// Every grant, failed ones included, lands in the history.
func TestGiveFreeTime(t *testing.T) {
	conn := messageDetailTestDB(t)
	ctx := context.Background()
	adminID := insertID(t, conn, `INSERT INTO admin_users (email, password_hash, name, role) VALUES ('nick@example.invalid', '!', 'Nick', 'superadmin') RETURNING id`)
	admin := &AdminUser{ID: adminID, Email: "nick@example.invalid", Role: "superadmin"}
	renews := time.Now().UTC().AddDate(0, 5, 0).Truncate(time.Second)

	appleUser := insertID(t, conn, `INSERT INTO users (user_id, email, password, subscription_valid, subscription_expiry, subscription_platform, subscription_product_id, apple_original_transaction_id)
		VALUES (gen_random_uuid(), 'apple@example.invalid', '!', true, $1, 'apple', 'io.devlan.rfspager.yearly', '5000001') RETURNING id`, renews)
	googleUser := insertID(t, conn, `INSERT INTO users (user_id, email, password, subscription_valid, subscription_expiry, subscription_platform, subscription_product_id, google_purchase_token)
		VALUES (gen_random_uuid(), 'google@example.invalid', '!', true, $1, 'google', 'io.devlan.rfspager.monthly', 'tok-1') RETURNING id`, renews)
	lapsed := insertID(t, conn, `INSERT INTO users (user_id, email, password, subscription_valid, subscription_expiry, subscription_platform, subscription_product_id, apple_original_transaction_id)
		VALUES (gen_random_uuid(), 'lapsed@example.invalid', '!', false, NOW() - INTERVAL '40 days', 'apple', 'io.devlan.rfspager.monthly', '5000002') RETURNING id`)
	mustExec(t, conn, `UPDATE users SET cancellation_reason = 'voluntarily_cancelled' WHERE id = $1`, lapsed)
	gifted := insertID(t, conn, `INSERT INTO users (user_id, email, password, subscription_valid, subscription_expiry, subscription_product_id)
		VALUES (gen_random_uuid(), 'gifted@example.invalid', '!', true, $1, 'io.devlan.rfspager.gifted') RETURNING id`, renews)

	apple := &fakeApple{expiry: renews}
	google := &fakeGoogle{expiry: renews}
	h := newExtensionHandlers(conn, Extenders{Apple: apple, Google: google})

	// Validation: a note is required, and Apple caps a grant at 180 days.
	if code, _ := grantRequest(t, h, admin, appleUser, map[string]any{"days": 30, "note": "  "}); code != http.StatusBadRequest {
		t.Fatalf("empty note: %d", code)
	}
	if code, _ := grantRequest(t, h, admin, appleUser, map[string]any{"days": 181, "note": "x"}); code != http.StatusBadRequest {
		t.Fatalf("181 Apple days: %d", code)
	}
	if got := listExtensions(t, h, appleUser); got.Method != "apple" || got.AppleExtensionsLeft != 2 || got.MaxDays != 180 {
		t.Fatalf("apple preview = %+v", got)
	}

	// Apple, 120 days: two calls (90 + 30), both recorded on one row.
	code, out := grantRequest(t, h, admin, appleUser, map[string]any{"days": 120, "note": "Ticket #133", "reason": 3})
	if code != http.StatusCreated {
		t.Fatalf("apple grant: %d %v", code, out)
	}
	if len(apple.calls) != 2 || apple.calls[0] != 90 || apple.calls[1] != 30 {
		t.Fatalf("apple calls = %v", apple.calls)
	}
	list := listExtensions(t, h, appleUser)
	if list.AppleExtensionsLeft != 0 || len(list.Data) != 1 {
		t.Fatalf("after apple grant: %+v", list)
	}
	row := list.Data[0]
	if row.Method != "apple" || row.Status != "applied" || row.DaysApplied != 120 || len(row.StoreRequests) != 2 ||
		row.ReasonCode == nil || *row.ReasonCode != 3 || row.AdminEmail == nil || *row.AdminEmail != "nick@example.invalid" ||
		row.ExpiryBefore == nil || !row.ExpiryBefore.Equal(renews) || row.ExpiryAfter == nil || !row.ExpiryAfter.Equal(renews.AddDate(0, 0, 120)) {
		t.Fatalf("apple row = %+v", row)
	}
	// The allowance is used up: refused before calling Apple, nothing recorded.
	if code, _ := grantRequest(t, h, admin, appleUser, map[string]any{"days": 10, "note": "again"}); code != http.StatusConflict {
		t.Fatalf("third Apple extension: %d", code)
	}
	if len(apple.calls) != 2 || len(listExtensions(t, h, appleUser).Data) != 1 {
		t.Fatalf("refused grant still called Apple or recorded a row")
	}

	// Google: one deferral.
	if code, out := grantRequest(t, h, admin, googleUser, map[string]any{"days": 60, "note": "goodwill"}); code != http.StatusCreated {
		t.Fatalf("google grant: %d %v", code, out)
	}
	if len(google.days) != 1 || google.days[0] != 60 {
		t.Fatalf("google deferrals = %v", google.days)
	}
	if g := listExtensions(t, h, googleUser).Data[0]; g.Method != "google" || g.Status != "applied" || g.ReasonCode != nil || !g.ExpiryAfter.Equal(renews.AddDate(0, 0, 60)) {
		t.Fatalf("google row = %+v", g)
	}

	// Lapsed store subscriber: a gift from now, with an audit row.
	before := time.Now()
	if code, out := grantRequest(t, h, admin, lapsed, map[string]any{"days": 30, "note": "come back"}); code != http.StatusCreated {
		t.Fatalf("gift grant: %d %v", code, out)
	}
	var u struct {
		Valid    bool      `db:"subscription_valid"`
		Expiry   time.Time `db:"subscription_expiry"`
		Product  string    `db:"subscription_product_id"`
		Platform *string   `db:"subscription_platform"`
		Renew    *bool     `db:"will_renew"`
		Reason   *string   `db:"cancellation_reason"`
	}
	if err := conn.GetContext(ctx, &u, `SELECT subscription_valid, subscription_expiry, subscription_product_id, subscription_platform, will_renew, cancellation_reason FROM users WHERE id = $1`, lapsed); err != nil {
		t.Fatal(err)
	}
	want := before.AddDate(0, 0, 30)
	if !u.Valid || u.Product != "io.devlan.rfspager.gifted" || u.Platform != nil || u.Renew != nil || u.Reason != nil ||
		u.Expiry.Before(want.Add(-time.Minute)) || u.Expiry.After(want.Add(time.Minute)) {
		t.Fatalf("gifted user = %+v", u)
	}
	var audit int
	_ = conn.GetContext(ctx, &audit, `SELECT count(*) FROM iap_transactions WHERE user_id = $1 AND event_type = 'admin:gift' AND platform = 'gift'`, lapsed)
	if audit != 1 {
		t.Fatalf("gift audit rows = %d", audit)
	}

	// Gift on a running gift: extends from its expiry, not from now.
	if code, out := grantRequest(t, h, admin, gifted, map[string]any{"days": 10, "note": "more"}); code != http.StatusCreated {
		t.Fatalf("gift extension: %d %v", code, out)
	}
	if err := conn.GetContext(ctx, &u.Expiry, `SELECT subscription_expiry FROM users WHERE id = $1`, gifted); err != nil {
		t.Fatal(err)
	}
	if !u.Expiry.Equal(renews.AddDate(0, 0, 10)) {
		t.Fatalf("running gift extended to %v, want %v", u.Expiry, renews.AddDate(0, 0, 10))
	}

	// A store refusal is recorded as failed (or partial) and reported as 502.
	apple2 := &fakeApple{expiry: renews, failOn: 2}
	h2 := newExtensionHandlers(conn, Extenders{Apple: apple2, Google: google})
	second := insertID(t, conn, `INSERT INTO users (user_id, email, password, subscription_valid, subscription_expiry, subscription_platform, subscription_product_id, apple_original_transaction_id)
		VALUES (gen_random_uuid(), 'apple2@example.invalid', '!', true, $1, 'apple', 'io.devlan.rfspager.yearly', '5000003') RETURNING id`, renews)
	if code, _ := grantRequest(t, h2, admin, second, map[string]any{"days": 180, "note": "six months"}); code != http.StatusBadGateway {
		t.Fatalf("partial grant: %d", code)
	}
	list = listExtensions(t, h2, second)
	if p := list.Data[0]; p.Status != "partial" || p.DaysApplied != 90 || len(p.StoreRequests) != 1 || p.Error == nil || list.AppleExtensionsLeft != 1 {
		t.Fatalf("partial row = %+v left=%d", p, list.AppleExtensionsLeft)
	}

	// The user list and detail show when free time was last given.
	detail, err := NewStore(conn).GetUser(ctx, appleUser)
	if err != nil {
		t.Fatal(err)
	}
	if detail["free_time_days"] != int64(120) {
		t.Fatalf("user detail free_time_days = %#v", detail["free_time_days"])
	}
}
