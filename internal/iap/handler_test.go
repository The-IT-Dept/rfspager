package iap

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWriteSubscriptionErrorReturnsConflictForOtherUsersPurchase(t *testing.T) {
	r := httptest.NewRecorder()
	writeSubscriptionError(r, &OwnershipConflictError{Platform: "apple", Identifier: "private"})
	if r.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", r.Code, http.StatusConflict)
	}
	if got := r.Body.String(); got != "{\"error\":\"subscription is already linked to another account\"}\n" {
		t.Fatalf("body = %q", got)
	}
}

func TestAppleTransactionResultMapsReceiptIdentityAndEnvironment(t *testing.T) {
	txn := &AppleJWSTransactionPayload{
		TransactionID:         "apple-transaction",
		OriginalTransactionID: "apple-original",
		ProductID:             "pro.monthly",
		Environment:           "Production",
		ExpiresDate:           time.Now().Add(time.Hour).UnixMilli(),
	}
	result := appleTransactionResult(txn)
	if result.ProductID != txn.ProductID || result.TransactionID != txn.TransactionID || result.OriginalTransactionID != txn.OriginalTransactionID {
		t.Fatalf("receipt identity mapping = %#v", result)
	}
	if result.Environment != "Production" {
		t.Fatalf("receipt environment = %q, want Production", result.Environment)
	}
	opts := Options{AllowedProductIDs: []string{"pro.monthly"}, AppleEnvironment: "Production", GoogleEnvironment: "Production"}
	if err := opts.validate(result); err != nil {
		t.Fatalf("mapped production receipt rejected: %v", err)
	}
}
