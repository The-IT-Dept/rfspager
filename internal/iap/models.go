package iap

import (
	"fmt"
	"strings"
	"time"
)

// ValidationResult is the unified result of validating a purchase from either store.
type ValidationResult struct {
	Valid                 bool
	ProductID             string
	Platform              string // "apple" or "google"
	Environment           string // Apple: Production/Sandbox; Google: Production/Test
	ExpiresAt             *time.Time
	TransactionID         string // Apple: transactionId, Google: orderId
	OriginalTransactionID string // Apple: originalTransactionId, Google: purchaseToken
	PreviousOwnershipID   string // Google: linkedPurchaseToken for upgrade/downgrade continuity
	WillRenew             bool
	IsTrialPeriod         bool
}

// Options declares the store products and environments this process may grant.
// An empty allowlist is intentionally not permissive: deploying without an
// explicit store configuration must not turn arbitrary valid receipts into
// premium access.
type Options struct {
	AllowedProductIDs []string
	AppleEnvironment  string // "Production" or "Sandbox"
	GoogleEnvironment string // "Production" or "Test"
}

func (o Options) validate(result *ValidationResult) error {
	if result == nil {
		return fmt.Errorf("missing validation result")
	}
	if result.ProductID == "" {
		return fmt.Errorf("store validation returned no product ID")
	}
	allowed := make(map[string]struct{}, len(o.AllowedProductIDs))
	for _, productID := range o.AllowedProductIDs {
		if productID = strings.TrimSpace(productID); productID != "" {
			allowed[productID] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return fmt.Errorf("IAP product allowlist is not configured")
	}
	if _, ok := allowed[result.ProductID]; !ok {
		return fmt.Errorf("product %q is not allowed", result.ProductID)
	}

	var expected string
	switch result.Platform {
	case "apple":
		expected = o.AppleEnvironment
		if expected != "Production" && expected != "Sandbox" {
			return fmt.Errorf("Apple IAP environment must be Production or Sandbox")
		}
	case "google":
		expected = o.GoogleEnvironment
		if expected != "Production" && expected != "Test" {
			return fmt.Errorf("Google IAP environment must be Production or Test")
		}
	default:
		return fmt.Errorf("unsupported IAP platform %q", result.Platform)
	}
	if result.Environment != expected {
		return fmt.Errorf("%s receipt environment %q does not match configured %q", result.Platform, result.Environment, expected)
	}
	if result.OriginalTransactionID == "" {
		return fmt.Errorf("store validation returned no ownership identifier")
	}
	return nil
}

// AppleValidateRequest is the request from the app after an Apple purchase.
type AppleValidateRequest struct {
	TransactionID  string `json:"transaction_id"`
	JWSTransaction string `json:"jws_transaction"` // JWS signed transaction from StoreKit 2
}

// GoogleValidateRequest is the request from the app after a Google Play purchase.
type GoogleValidateRequest struct {
	PurchaseToken string `json:"purchase_token"`
	ProductID     string `json:"product_id"`
}

// RestoreRequest is the request to restore purchases.
type RestoreRequest struct {
	Platform string `json:"platform"` // "ios" or "android"
	// iOS: array of JWS signed transactions (preferred)
	JWSTransactions []string `json:"jws_transactions,omitempty"`
	// iOS: array of transaction IDs (fallback, needs Apple API credentials)
	TransactionIDs []string `json:"transaction_ids,omitempty"`
	// Android: array of purchase tokens with product IDs
	Purchases []GoogleValidateRequest `json:"purchases,omitempty"`
}

// AppleJWSTransactionPayload is the decoded payload from Apple's signed transaction.
type AppleJWSTransactionPayload struct {
	TransactionID         string `json:"transactionId"`
	OriginalTransactionID string `json:"originalTransactionId"`
	BundleID              string `json:"bundleId"`
	ProductID             string `json:"productId"`
	PurchaseDate          int64  `json:"purchaseDate"`
	ExpiresDate           int64  `json:"expiresDate"`
	Type                  string `json:"type"`               // "Auto-Renewable Subscription"
	InAppOwnershipType    string `json:"inAppOwnershipType"` // "PURCHASED"
	RevocationDate        *int64 `json:"revocationDate"`
	IsUpgraded            bool   `json:"isUpgraded"`
	OfferType             *int   `json:"offerType"`
	Environment           string `json:"environment"` // "Production" or "Sandbox"
}

// AppleNotificationPayload is the decoded payload from Apple's S2S notification.
type AppleNotificationPayload struct {
	NotificationType string `json:"notificationType"`
	Subtype          string `json:"subtype"`
	Data             struct {
		SignedTransactionInfo string `json:"signedTransactionInfo"`
		SignedRenewalInfo     string `json:"signedRenewalInfo"`
	} `json:"data"`
}

// AppleRenewalInfoPayload is the decoded renewal info.
type AppleRenewalInfoPayload struct {
	AutoRenewProductID     string `json:"autoRenewProductId"`
	AutoRenewStatus        int    `json:"autoRenewStatus"` // 1 = will renew
	ExpirationIntent       *int   `json:"expirationIntent"`
	GracePeriodExpiresDate int64  `json:"gracePeriodExpiresDate"` // ms; set while in billing grace
}
