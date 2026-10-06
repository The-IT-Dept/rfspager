package iap

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testAdaptyDB(t *testing.T) *AdaptyWebhookHandler {
	t.Helper()
	db := testOwnershipDB(t)
	if _, err := db.Exec(`
		ALTER TABLE users
			ADD COLUMN user_id UUID UNIQUE,
			ADD COLUMN adapty_id VARCHAR(255),
			ADD COLUMN last_adapty_sync_at TIMESTAMPTZ;
		UPDATE users SET user_id = 'aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee', will_renew = true WHERE id = 1;
		UPDATE users SET user_id = 'aaaaaaaa-bbbb-4ccc-8ddd-ffffffffffff', will_renew = true WHERE id = 2;
	`); err != nil {
		t.Fatalf("extend fixture: %v", err)
	}
	return &AdaptyWebhookHandler{db: db}
}

func postAdapty(t *testing.T, h *AdaptyWebhookHandler, body string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	h.Handle(rec, httptest.NewRequest(http.MethodPost, "/api/adapty-webhook", strings.NewReader(body)))
	return rec.Code
}

func TestAdaptyWebhookAppliesAppleEvent(t *testing.T) {
	h := testAdaptyDB(t)
	if code := postAdapty(t, h, adaptyAppleAccessLevelUpdated); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	var row struct {
		Valid    bool           `db:"subscription_valid"`
		Platform sql.NullString `db:"subscription_platform"`
		Apple    sql.NullString `db:"apple_original_transaction_id"`
		Renew    sql.NullBool   `db:"will_renew"`
		Adapty   sql.NullString `db:"adapty_id"`
		Product  sql.NullString `db:"subscription_product_id"`
	}
	if err := h.db.Get(&row, `SELECT subscription_valid, subscription_platform, apple_original_transaction_id,
		will_renew, adapty_id, subscription_product_id FROM users WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if !row.Valid || row.Platform.String != "apple" || row.Apple.String != "230003643191093" ||
		row.Renew.Bool || row.Adapty.String != "11111111-2222-4333-8444-555555555555" ||
		row.Product.String != "io.devlan.rfspager.monthly" {
		t.Fatalf("user row: %+v", row)
	}
	var linked int
	_ = h.db.Get(&linked, `SELECT count(*) FROM iap_subscription_ownership WHERE platform='apple' AND original_transaction_id='230003643191093' AND user_id=1`)
	var audit int
	_ = h.db.Get(&audit, `SELECT count(*) FROM iap_transactions WHERE user_id=1 AND event_type='adapty:access_level_updated'`)
	if linked != 1 || audit != 1 {
		t.Fatalf("ledger=%d audit=%d", linked, audit)
	}

	// Redelivery is idempotent for the ledger.
	if code := postAdapty(t, h, adaptyAppleAccessLevelUpdated); code != http.StatusOK {
		t.Fatalf("redelivery status %d", code)
	}
	_ = h.db.Get(&linked, `SELECT count(*) FROM iap_subscription_ownership WHERE user_id=1`)
	if linked != 1 {
		t.Fatalf("ledger rows after redelivery: %d", linked)
	}
}

func TestAdaptyWebhookAppliesGooglePurchaseTokenAndKeepsWillRenew(t *testing.T) {
	h := testAdaptyDB(t)
	if code := postAdapty(t, h, adaptyGoogleRenewed); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	var token sql.NullString
	var renew sql.NullBool
	if err := h.db.QueryRow(`SELECT google_purchase_token, will_renew FROM users WHERE id = 2`).Scan(&token, &renew); err != nil {
		t.Fatal(err)
	}
	if token.String != "hkdpolajbgnmmexamplepurchasetoken" {
		t.Fatalf("google token %q", token.String)
	}
	if !renew.Valid || !renew.Bool {
		t.Fatalf("will_renew must be untouched by events that omit it: %+v", renew)
	}
}

func TestAdaptyWebhookUnknownUserIsAcknowledged(t *testing.T) {
	h := testAdaptyDB(t)
	_, err := h.apply(context.Background(), adaptyUpdate{CustomerUserID: "aaaaaaaa-bbbb-4ccc-8ddd-000000000000", HasSubscription: true}, "{}")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("want ErrNoRows, got %v", err)
	}
	body := strings.Replace(adaptyAppleAccessLevelUpdated, "eeeeeeeeeeee", "000000000000", 1)
	if code := postAdapty(t, h, body); code != http.StatusOK {
		t.Fatalf("unknown user should 200 so Adapty stops retrying, got %d", code)
	}
}
