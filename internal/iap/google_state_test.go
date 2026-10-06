package iap

import (
	"testing"
	"time"

	"google.golang.org/api/androidpublisher/v3"
)

func googlePurchaseIn(state string, expiry time.Time, autoRenew bool) *androidpublisher.SubscriptionPurchaseV2 {
	return &androidpublisher.SubscriptionPurchaseV2{
		SubscriptionState: state,
		LineItems: []*androidpublisher.SubscriptionPurchaseLineItem{{
			ProductId:        "io.devlan.rfspager.monthly",
			ExpiryTime:       expiry.UTC().Format(time.RFC3339),
			AutoRenewingPlan: &androidpublisher.AutoRenewingPlan{AutoRenewEnabled: autoRenew},
		}},
	}
}

func TestGoogleStateValidity(t *testing.T) {
	future := time.Now().Add(10 * 24 * time.Hour)
	past := time.Now().Add(-24 * time.Hour)
	cases := []struct {
		name      string
		state     string
		expiry    time.Time
		autoRenew bool
		valid     bool
		willRenew bool
	}{
		{"active", "SUBSCRIPTION_STATE_ACTIVE", future, true, true, true},
		{"grace period", "SUBSCRIPTION_STATE_IN_GRACE_PERIOD", future, true, true, true},
		// A cancelled subscriber keeps what they paid for until expiry.
		{"cancelled, paid period left", "SUBSCRIPTION_STATE_CANCELED", future, false, true, false},
		{"cancelled, period over", "SUBSCRIPTION_STATE_CANCELED", past, false, false, false},
		{"on hold (payment failed)", "SUBSCRIPTION_STATE_ON_HOLD", future, true, false, true},
		{"paused", "SUBSCRIPTION_STATE_PAUSED", future, true, false, true},
		{"expired", "SUBSCRIPTION_STATE_EXPIRED", past, false, false, false},
		{"pending", "SUBSCRIPTION_STATE_PENDING", future, true, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := googleValidationResult(googlePurchaseIn(tc.state, tc.expiry, tc.autoRenew), "token", "")
			if err != nil {
				t.Fatal(err)
			}
			if r.Valid != tc.valid || r.WillRenew != tc.willRenew {
				t.Fatalf("valid=%v willRenew=%v, want valid=%v willRenew=%v", r.Valid, r.WillRenew, tc.valid, tc.willRenew)
			}
		})
	}
}
