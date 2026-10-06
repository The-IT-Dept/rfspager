package iap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Payload shapes mirror real Adapty events from production logs; identifiers
// are synthetic.
const adaptyAppleAccessLevelUpdated = `{
  "profile_id": "11111111-2222-4333-8444-555555555555",
  "customer_user_id": "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
  "event_type": "access_level_updated",
  "event_datetime": "2026-09-24T05:46:18.000000+0000",
  "event_properties": {
    "access_level_id": "premium",
    "store": "app_store",
    "environment": "Production",
    "vendor_product_id": "io.devlan.rfspager.monthly",
    "transaction_id": "230003643191999",
    "original_transaction_id": "230003643191093",
    "subscription_expires_at": "2026-10-24T05:46:18.000000+0000",
    "expires_at": "2026-10-24T05:46:18.000000+0000",
    "is_active": true,
    "is_in_grace_period": false,
    "is_refund": false,
    "will_renew": false,
    "cancellation_reason": "voluntarily_cancelled"
  },
  "play_store_purchase_token": null
}`

const adaptyGoogleRenewed = `{
  "profile_id": "11111111-2222-4333-8444-666666666666",
  "customer_user_id": "aaaaaaaa-bbbb-4ccc-8ddd-ffffffffffff",
  "event_type": "subscription_renewed",
  "event_properties": {
    "store": "play_store",
    "environment": "Production",
    "vendor_product_id": "io.devlan.rfspager.yearly",
    "transaction_id": "GPA.1234-5678-9012-34567..3",
    "original_transaction_id": "GPA.1234-5678-9012-34567",
    "subscription_expires_at": "2027-09-24T05:46:18.000000+0000"
  },
  "play_store_purchase_token": {
    "product_id": "io.devlan.rfspager.yearly",
    "purchase_token": "hkdpolajbgnmmexamplepurchasetoken",
    "is_subscription": true
  }
}`

func mustEvent(t *testing.T, raw string) *adaptyEvent {
	t.Helper()
	var ev adaptyEvent
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return &ev
}

var adaptyNow = time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)

func TestDeriveAdaptyUpdateApple(t *testing.T) {
	u := deriveAdaptyUpdate(mustEvent(t, adaptyAppleAccessLevelUpdated), adaptyNow)
	if u.Platform != "apple" || u.StoreOriginalID != "230003643191093" || u.StoreTransactionID != "230003643191999" {
		t.Fatalf("store ids: %+v", u)
	}
	if !u.HasSubscription || !u.Valid {
		t.Fatalf("want valid subscription: %+v", u)
	}
	if u.WillRenew == nil || *u.WillRenew {
		t.Fatalf("will_renew should be false: %+v", u.WillRenew)
	}
	if u.CancellationReason != "voluntarily_cancelled" || u.CustomerUserID != "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee" {
		t.Fatalf("fields: %+v", u)
	}
	if want := time.Date(2026, 10, 24, 5, 46, 18, 0, time.UTC); !u.Expiry.Equal(want) {
		t.Fatalf("expiry %v want %v", u.Expiry, want)
	}
}

func TestDeriveAdaptyUpdateGoogleUsesPurchaseTokenNotOrderID(t *testing.T) {
	u := deriveAdaptyUpdate(mustEvent(t, adaptyGoogleRenewed), adaptyNow)
	if u.Platform != "google" || u.StoreOriginalID != "hkdpolajbgnmmexamplepurchasetoken" {
		t.Fatalf("google store id: %+v", u)
	}
	if u.StoreTransactionID != "GPA.1234-5678-9012-34567..3" {
		t.Fatalf("txn id: %q", u.StoreTransactionID)
	}
	// No is_active on this event type → validity from expiry; no will_renew → untouched.
	if !u.Valid || u.WillRenew != nil {
		t.Fatalf("valid/will_renew: %+v", u)
	}
}

func TestDeriveAdaptyUpdateValidity(t *testing.T) {
	future := "2026-11-01T00:00:00.000000+0000"
	past := "2026-09-01T00:00:00.000000+0000"
	cases := []struct {
		name  string
		props string
		event string
		want  bool
	}{
		{"expired, no is_active", `"subscription_expires_at":"` + past + `"`, "subscription_expired", false},
		{"future, no is_active", `"subscription_expires_at":"` + future + `"`, "subscription_renewed", true},
		{"is_active false wins over future expiry", `"subscription_expires_at":"` + future + `","is_active":false`, "access_level_updated", false},
		{"grace: is_active true with past expiry", `"subscription_expires_at":"` + past + `","is_active":true,"is_in_grace_period":true`, "access_level_updated", true},
		{"refund flag", `"subscription_expires_at":"` + future + `","is_active":true,"is_refund":true`, "access_level_updated", false},
		{"refund event", `"subscription_expires_at":"` + future + `"`, "subscription_refunded", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{"customer_user_id":"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee","event_type":"` + tc.event +
				`","event_properties":{"store":"app_store",` + tc.props + `}}`
			u := deriveAdaptyUpdate(mustEvent(t, raw), adaptyNow)
			if u.Valid != tc.want {
				t.Fatalf("valid=%v want %v", u.Valid, tc.want)
			}
		})
	}
}

func TestDeriveAdaptyUpdateWithoutExpiryOnlyTouchesProfile(t *testing.T) {
	u := deriveAdaptyUpdate(mustEvent(t, `{"customer_user_id":"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee","profile_id":"p","event_type":"non_subscription_purchase","event_properties":{"store":"app_store"}}`), adaptyNow)
	if u.HasSubscription {
		t.Fatalf("no expiry must not touch subscription fields: %+v", u)
	}
}

func TestAdaptyWebhookCheckHandshake(t *testing.T) {
	h := &AdaptyWebhookHandler{}
	rec := httptest.NewRecorder()
	h.Handle(rec, httptest.NewRequest(http.MethodPost, "/api/adapty-webhook", strings.NewReader(`{"adapty_check":"abc123"}`)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"adapty_check_response":"abc123"`) {
		t.Fatalf("handshake: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAdaptyWebhookRequiresConfiguredAuth(t *testing.T) {
	h := &AdaptyWebhookHandler{authHeader: "s3cret"}
	rec := httptest.NewRecorder()
	h.Handle(rec, httptest.NewRequest(http.MethodPost, "/api/adapty-webhook", strings.NewReader(`{"adapty_check":"x"}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth: %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/adapty-webhook", strings.NewReader(`{"adapty_check":"x"}`))
	req.Header.Set("Authorization", "s3cret")
	rec = httptest.NewRecorder()
	h.Handle(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid auth: %d", rec.Code)
	}
}

func TestAdaptyWebhookIgnoresEventsWithoutCustomer(t *testing.T) {
	h := &AdaptyWebhookHandler{} // nil db: must not be touched
	rec := httptest.NewRecorder()
	h.Handle(rec, httptest.NewRequest(http.MethodPost, "/api/adapty-webhook",
		strings.NewReader(`{"customer_user_id":null,"event_type":"access_level_updated","event_properties":{"store":"app_store"}}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("anonymous profile: %d", rec.Code)
	}
}
