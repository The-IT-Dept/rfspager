package iap

import (
	"strings"
	"testing"
	"time"

	"google.golang.org/api/androidpublisher/v3"
)

func activeResult(platform, productID, environment string) *ValidationResult {
	expires := time.Now().Add(time.Hour)
	return &ValidationResult{
		Valid:                 true,
		Platform:              platform,
		ProductID:             productID,
		Environment:           environment,
		OriginalTransactionID: "ownership-id",
		ExpiresAt:             &expires,
	}
}

func TestOptionsRejectsUnconfiguredProductAllowlist(t *testing.T) {
	err := Options{}.validate(activeResult("apple", "pro.monthly", "Production"))
	if err == nil || !strings.Contains(err.Error(), "product") {
		t.Fatalf("unconfigured allowlist error = %v", err)
	}
}

func TestOptionsRejectsUnexpectedAppleEnvironment(t *testing.T) {
	opts := Options{AllowedProductIDs: []string{"pro.monthly"}, AppleEnvironment: "Production", GoogleEnvironment: "Production"}
	err := opts.validate(activeResult("apple", "pro.monthly", "Sandbox"))
	if err == nil || !strings.Contains(err.Error(), "environment") {
		t.Fatalf("sandbox receipt error = %v", err)
	}
}

func TestOptionsAcceptsConfiguredGoogleProductionPurchase(t *testing.T) {
	opts := Options{AllowedProductIDs: []string{"pro.monthly"}, AppleEnvironment: "Production", GoogleEnvironment: "Production"}
	if err := opts.validate(activeResult("google", "pro.monthly", "Production")); err != nil {
		t.Fatalf("configured production purchase rejected: %v", err)
	}
}

func TestGoogleResultUsesStoreProductAndRejectsCallerMismatch(t *testing.T) {
	purchase := &androidpublisher.SubscriptionPurchaseV2{
		SubscriptionState: "SUBSCRIPTION_STATE_ACTIVE",
		LineItems: []*androidpublisher.SubscriptionPurchaseLineItem{{
			ProductId:               "pro.monthly",
			LatestSuccessfulOrderId: "GPA.1234",
			OfferDetails:            &androidpublisher.OfferDetails{BasePlanId: "monthly-base-plan"},
		}},
	}

	result, err := googleValidationResult(purchase, "token-123", "")
	if err != nil {
		t.Fatalf("parse Google result: %v", err)
	}
	if result.ProductID != "pro.monthly" {
		t.Fatalf("product ID = %q, want store product", result.ProductID)
	}
	if result.TransactionID != "GPA.1234" {
		t.Fatalf("transaction ID = %q, want store order ID", result.TransactionID)
	}

	_, err = googleValidationResult(purchase, "token-123", "attacker-selected-product")
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("caller product mismatch error = %v", err)
	}
}
