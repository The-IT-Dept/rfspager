package iap

import (
	"testing"
	"time"
)

func TestAppleStatusResult(t *testing.T) {
	now := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	future := now.Add(20 * 24 * time.Hour).UnixMilli()
	past := now.Add(-2 * 24 * time.Hour).UnixMilli()
	grace := now.Add(6 * 24 * time.Hour).UnixMilli()
	revoked := past

	renew := func(on bool) *AppleRenewalInfoPayload {
		r := &AppleRenewalInfoPayload{}
		if on {
			r.AutoRenewStatus = 1
		}
		return r
	}
	cases := []struct {
		name       string
		status     int
		expires    int64
		revocation *int64
		renewal    *AppleRenewalInfoPayload
		valid      bool
		willRenew  bool
		wantExpiry int64
	}{
		{"active, renewing", appleStatusActive, future, nil, renew(true), true, true, future},
		{"active, auto-renew off", appleStatusActive, future, nil, renew(false), true, false, future},
		{"grace period uses grace expiry", appleStatusGracePeriod, past, nil,
			&AppleRenewalInfoPayload{AutoRenewStatus: 1, GracePeriodExpiresDate: grace}, true, true, grace},
		{"billing retry", appleStatusBillingRetry, past, nil, renew(true), false, true, past},
		{"expired", appleStatusExpired, past, nil, renew(false), false, false, past},
		{"revoked status", appleStatusRevoked, future, nil, renew(false), false, false, future},
		{"refunded transaction", appleStatusActive, future, &revoked, renew(true), false, true, future},
		{"no renewal info", appleStatusActive, future, nil, nil, true, false, future},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			txn := &AppleJWSTransactionPayload{
				TransactionID: "2", OriginalTransactionID: "1", ProductID: "io.devlan.rfspager.monthly",
				ExpiresDate: tc.expires, RevocationDate: tc.revocation, Environment: "Production",
			}
			r := appleStatusResult(tc.status, txn, tc.renewal, now)
			if r.Valid != tc.valid || r.WillRenew != tc.willRenew {
				t.Fatalf("valid=%v willRenew=%v, want valid=%v willRenew=%v", r.Valid, r.WillRenew, tc.valid, tc.willRenew)
			}
			if r.ExpiresAt == nil || r.ExpiresAt.UnixMilli() != tc.wantExpiry {
				t.Fatalf("expiry %v, want %v", r.ExpiresAt, time.UnixMilli(tc.wantExpiry))
			}
			if r.OriginalTransactionID != "1" || r.Platform != "apple" {
				t.Fatalf("identity: %+v", r)
			}
		})
	}
}

func TestPreferAppleResultPrefersEntitlementThenExpiry(t *testing.T) {
	soon, later := time.Now().Add(time.Hour), time.Now().Add(48*time.Hour)
	activeSoon := &ValidationResult{Valid: true, ExpiresAt: &soon}
	expiredLater := &ValidationResult{Valid: false, ExpiresAt: &later}
	activeLater := &ValidationResult{Valid: true, ExpiresAt: &later}
	if !preferAppleResult(activeSoon, expiredLater) {
		t.Fatal("an entitling subscription must beat a lapsed one with a later expiry")
	}
	if !preferAppleResult(activeLater, activeSoon) {
		t.Fatal("among entitling subscriptions the later expiry wins")
	}
}
