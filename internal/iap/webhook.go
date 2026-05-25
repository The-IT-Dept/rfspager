package iap

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jmoiron/sqlx"
)

// WebhookHandler handles Apple S2S and Google RTDN notifications.
type WebhookHandler struct {
	db                  *sqlx.DB
	apple               *AppleClient
	google              *GoogleClient
	appleBundleID       string
	googlePackageName   string
	pubsubAudience      string
	pubsubServiceEmail  string
}

// WebhookConfig carries the values needed to authenticate inbound store webhooks.
type WebhookConfig struct {
	AppleBundleID      string
	GooglePackageName  string
	PubsubAudience     string // Pub/Sub push subscription audience (typically the webhook URL)
	PubsubServiceEmail string // Optional: service-account email expected on the OIDC token
}

func NewWebhookHandler(db *sqlx.DB, apple *AppleClient, google *GoogleClient, cfg WebhookConfig) *WebhookHandler {
	return &WebhookHandler{
		db:                 db,
		apple:              apple,
		google:             google,
		appleBundleID:      cfg.AppleBundleID,
		googlePackageName:  cfg.GooglePackageName,
		pubsubAudience:     cfg.PubsubAudience,
		pubsubServiceEmail: cfg.PubsubServiceEmail,
	}
}

// AppleNotification handles POST /webhooks/apple
// Apple sends a JWS-signed payload for subscription lifecycle events.
func (h *WebhookHandler) AppleNotification(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Apple sends {"signedPayload": "...JWS..."}
	var envelope struct {
		SignedPayload string `json:"signedPayload"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		slog.Error("apple webhook: invalid JSON", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	notif, err := DecodeAppleNotification(envelope.SignedPayload)
	if err != nil {
		slog.Error("apple webhook: signature verification failed", "error", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	slog.Info("apple webhook received", "type", notif.NotificationType, "subtype", notif.Subtype)

	// Decode + verify the transaction info JWS.
	txn, err := DecodeAppleJWS(notif.Data.SignedTransactionInfo)
	if err != nil {
		slog.Error("apple webhook: transaction JWS verification failed", "error", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Bundle ID must match — otherwise this notification is for a different app.
	if h.appleBundleID != "" && txn.BundleID != h.appleBundleID {
		slog.Warn("apple webhook: bundle ID mismatch", "got", txn.BundleID, "want", h.appleBundleID)
		http.Error(w, "wrong bundle", http.StatusBadRequest)
		return
	}

	// Decode + verify renewal info (carries auto-renew status + grace-period intent).
	willRenew := true
	var expirationIntent int
	if notif.Data.SignedRenewalInfo != "" {
		renewal, err := DecodeRenewalInfo(notif.Data.SignedRenewalInfo)
		if err != nil {
			slog.Error("apple webhook: renewal info verification failed", "error", err)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		willRenew = renewal.AutoRenewStatus == 1
		if renewal.ExpirationIntent != nil {
			expirationIntent = *renewal.ExpirationIntent
		}
	}

	// Determine subscription validity based on notification type.
	// Apple grace-period semantics: when willRenew==true but past expiresDate,
	// Apple is retrying billing (expirationIntent 2 = billing error). Keep the
	// user valid for the grace window so they don't get downgraded mid-retry.
	now := time.Now()
	expiresAt := time.UnixMilli(txn.ExpiresDate)
	valid := expiresAt.After(now) && txn.RevocationDate == nil

	cancellationReason := ""
	switch notif.NotificationType {
	case "SUBSCRIBED", "DID_RENEW":
		// Active state — already covered by expiresAt > now.
	case "EXPIRED":
		valid = false
	case "REVOKE":
		valid = false
		cancellationReason = "revoked"
	case "REFUND":
		valid = false
		cancellationReason = "refund"
	case "GRACE_PERIOD_EXPIRED":
		valid = false
		cancellationReason = "grace_period_expired"
	case "DID_FAIL_TO_RENEW":
		// Billing-retry. expirationIntent == 2 means Apple is retrying;
		// give the user grace until GRACE_PERIOD_EXPIRED arrives.
		if expirationIntent == 2 {
			valid = true
		}
	case "DID_CHANGE_RENEWAL_STATUS":
		if notif.Subtype == "AUTO_RENEW_DISABLED" {
			willRenew = false
		}
	}

	// Update user by original_transaction_id
	result, err := h.db.ExecContext(r.Context(),
		`UPDATE users SET
			subscription_valid = $1,
			subscription_expiry = $2,
			subscription_product_id = $3,
			subscription_platform = 'apple',
			will_renew = $4,
			cancellation_reason = CASE WHEN $5 = '' THEN cancellation_reason ELSE $5 END,
			last_store_sync_at = NOW(),
			updated_at = NOW()
		 WHERE apple_original_transaction_id = $6`,
		valid, expiresAt, txn.ProductID, willRenew, cancellationReason, txn.OriginalTransactionID)
	if err != nil {
		slog.Error("apple webhook: update user failed", "error", err)
	} else {
		rows, _ := result.RowsAffected()
		slog.Info("apple webhook: user updated", "original_txn", txn.OriginalTransactionID, "valid", valid, "rows", rows)
	}

	// Log to iap_transactions
	var userID int64
	h.db.QueryRowContext(r.Context(),
		`SELECT id FROM users WHERE apple_original_transaction_id = $1`, txn.OriginalTransactionID).Scan(&userID)
	if userID > 0 {
		h.db.ExecContext(r.Context(),
			`INSERT INTO iap_transactions (user_id, platform, transaction_id, original_transaction_id, product_id, event_type, is_valid, expires_at, will_renew, raw_payload)
			 VALUES ($1, 'apple', $2, $3, $4, $5, $6, $7, $8, $9)`,
			userID, txn.TransactionID, txn.OriginalTransactionID, txn.ProductID,
			"webhook:"+notif.NotificationType, valid, &expiresAt, willRenew, string(body))
	}

	w.WriteHeader(http.StatusOK)
}

// GoogleNotification handles POST /webhooks/google
// Google RTDN sends Pub/Sub messages with subscription notifications.
// The Pub/Sub push subscription attaches an OIDC token in the Authorization
// header signed by a service account; we validate it before trusting the body.
func (h *WebhookHandler) GoogleNotification(w http.ResponseWriter, r *http.Request) {
	if h.pubsubAudience == "" {
		slog.Error("google webhook: GOOGLE_PUBSUB_AUDIENCE not configured, rejecting")
		http.Error(w, "webhook not configured", http.StatusServiceUnavailable)
		return
	}
	if err := verifyGooglePubsubOIDC(r.Context(), r, h.pubsubAudience, h.pubsubServiceEmail); err != nil {
		slog.Warn("google webhook: OIDC verification failed", "error", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Pub/Sub envelope: {"message": {"data": "base64...", "messageId": "..."}}
	var envelope struct {
		Message struct {
			Data      string `json:"data"`
			MessageID string `json:"messageId"`
		} `json:"message"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		slog.Error("google webhook: invalid JSON", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	data, err := base64.StdEncoding.DecodeString(envelope.Message.Data)
	if err != nil {
		slog.Error("google webhook: base64 decode failed", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var notification struct {
		PackageName              string `json:"packageName"`
		SubscriptionNotification struct {
			PurchaseToken    string `json:"purchaseToken"`
			SubscriptionID   string `json:"subscriptionId"`
			NotificationType int    `json:"notificationType"`
		} `json:"subscriptionNotification"`
	}
	if err := json.Unmarshal(data, &notification); err != nil {
		slog.Error("google webhook: unmarshal failed", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Package name must match — defends against a stranger forwarding a token.
	if h.googlePackageName != "" && notification.PackageName != "" && notification.PackageName != h.googlePackageName {
		slog.Warn("google webhook: package name mismatch", "got", notification.PackageName, "want", h.googlePackageName)
		http.Error(w, "wrong package", http.StatusBadRequest)
		return
	}

	slog.Info("google webhook received",
		"package", notification.PackageName,
		"type", notification.SubscriptionNotification.NotificationType,
		"subscription", notification.SubscriptionNotification.SubscriptionID,
		"message_id", envelope.Message.MessageID)

	if h.google == nil {
		slog.Warn("google webhook: no Google client configured, skipping validation")
		w.WriteHeader(http.StatusOK)
		return
	}

	// Re-validate the purchase with Google to get current state.
	purchaseToken := notification.SubscriptionNotification.PurchaseToken
	if purchaseToken == "" {
		// Other notification kinds (oneTimeProductNotification, testNotification)
		// don't carry a subscription token — ack and move on.
		w.WriteHeader(http.StatusOK)
		return
	}
	result, err := h.google.ValidatePurchase(context.Background(), purchaseToken, notification.SubscriptionNotification.SubscriptionID)
	if err != nil {
		slog.Error("google webhook: validation failed", "error", err)
		w.WriteHeader(http.StatusOK) // Ack to avoid Pub/Sub retries on transient API errors
		return
	}

	// Update user by google_purchase_token
	_, err = h.db.ExecContext(r.Context(),
		`UPDATE users SET
			subscription_valid = $1,
			subscription_expiry = $2,
			subscription_product_id = $3,
			subscription_platform = 'google',
			will_renew = $4,
			last_store_sync_at = NOW(),
			updated_at = NOW()
		 WHERE google_purchase_token = $5`,
		result.Valid, result.ExpiresAt, result.ProductID, result.WillRenew, purchaseToken)
	if err != nil {
		slog.Error("google webhook: update user failed", "error", err)
	}

	w.WriteHeader(http.StatusOK)
}
