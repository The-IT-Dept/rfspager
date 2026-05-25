package iap

import "time"

// ValidationResult is the unified result of validating a purchase from either store.
type ValidationResult struct {
	Valid                 bool
	ProductID             string
	Platform              string // "apple" or "google"
	ExpiresAt             *time.Time
	TransactionID         string // Apple: transactionId, Google: orderId
	OriginalTransactionID string // Apple: originalTransactionId, Google: purchaseToken
	WillRenew             bool
	IsTrialPeriod         bool
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
	Type                  string `json:"type"`                  // "Auto-Renewable Subscription"
	InAppOwnershipType    string `json:"inAppOwnershipType"`    // "PURCHASED"
	RevocationDate        *int64 `json:"revocationDate"`
	IsUpgraded            bool   `json:"isUpgraded"`
	OfferType             *int   `json:"offerType"`
	Environment           string `json:"environment"`           // "Production" or "Sandbox"
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
	AutoRenewProductID string `json:"autoRenewProductId"`
	AutoRenewStatus    int    `json:"autoRenewStatus"` // 1 = will renew
	ExpirationIntent   *int   `json:"expirationIntent"`
}
