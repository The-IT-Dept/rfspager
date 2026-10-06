package main

import (
	"encoding/json"
	"testing"
)

func profileWith(t *testing.T, store, original string) *adaptyProfile {
	t.Helper()
	var p adaptyProfile
	raw := `{"data":{"profile_id":"p","access_levels":[{"access_level_id":"premium","store":"` + store +
		`","store_product_id":"io.devlan.rfspager.monthly","store_original_transaction_id":"` + original +
		`","expires_at":"2099-01-01T00:00:00.000000+0000"}]}}`
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	return &p
}

func TestDerivePatchSkipsGoogleOrderID(t *testing.T) {
	p := derivePatch(profileWith(t, "play_store", "GPA.1234-5678-9012-34567"))
	if p == nil || p.Platform != "google" {
		t.Fatalf("patch: %+v", p)
	}
	if p.StoreOriginalID != "" {
		t.Fatalf("a Play order ID must not be stored as a purchase token: %q", p.StoreOriginalID)
	}
}

func TestDerivePatchKeepsAppleOriginalTransactionID(t *testing.T) {
	p := derivePatch(profileWith(t, "app_store", "230003643191093"))
	if p == nil || p.Platform != "apple" || p.StoreOriginalID != "230003643191093" {
		t.Fatalf("patch: %+v", p)
	}
}
