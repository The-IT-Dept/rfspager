package iap

import (
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
	db                 *sqlx.DB
	apple              *AppleClient
	google             *GoogleClient
	appleBundleID      string
	googlePackageName  string
	pubsubAudience     string
	pubsubServiceEmail string
	options            Options
}

// WebhookConfig carries the values needed to authenticate inbound store webhooks.
type WebhookConfig struct {
	AppleBundleID      string
	GooglePackageName  string
	PubsubAudience     string // Pub/Sub push subscription audience (typically the webhook URL)
	PubsubServiceEmail string // Optional: service-account email expected on the OIDC token
}

func NewWebhookHandler(db *sqlx.DB, apple *AppleClient, google *GoogleClient, cfg WebhookConfig, options ...Options) *WebhookHandler {
	var option Options
	if len(options) > 0 {
		option = options[0]
	}
	return &WebhookHandler{
		db:                 db,
		apple:              apple,
		google:             google,
		appleBundleID:      cfg.AppleBundleID,
		googlePackageName:  cfg.GooglePackageName,
		pubsubAudience:     cfg.PubsubAudience,
		pubsubServiceEmail: cfg.PubsubServiceEmail,
		options:            option,
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
	if !appleNotificationHasTransaction(notif) {
		// TEST (requested through the App Store Server API) and the
		// external-purchase types carry no transaction. Acknowledge them,
		// otherwise Apple records a failed attempt and keeps retrying.
		slog.Info("apple webhook: acknowledged without a transaction", "type", notif.NotificationType)
		w.WriteHeader(http.StatusOK)
		return
	}
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
	storeResult := &ValidationResult{
		Valid:                 valid,
		Platform:              "apple",
		Environment:           txn.Environment,
		ProductID:             txn.ProductID,
		ExpiresAt:             &expiresAt,
		TransactionID:         txn.TransactionID,
		OriginalTransactionID: txn.OriginalTransactionID,
		WillRenew:             willRenew,
	}
	if err := h.options.validate(storeResult); err != nil {
		slog.Warn("apple webhook: rejected store result", "error", err)
		http.Error(w, "unaccepted subscription", http.StatusBadRequest)
		return
	}

	updated, err := applyWebhookSubscription(r.Context(), h.db, storeResult,
		"webhook:"+notif.NotificationType, string(body), cancellationReason)
	if err != nil {
		slog.Error("apple webhook: applying subscription", "error", err)
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	slog.Info("apple webhook processed", "original_txn", txn.OriginalTransactionID, "updated_current", updated)

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
	result, err := h.google.ValidatePurchase(r.Context(), purchaseToken, notification.SubscriptionNotification.SubscriptionID)
	if err != nil {
		slog.Error("google webhook: validation failed", "error", err)
		w.WriteHeader(http.StatusOK) // Ack to avoid Pub/Sub retries on transient API errors
		return
	}
	if err := h.options.validate(result); err != nil {
		slog.Warn("google webhook: rejected store result", "error", err)
		http.Error(w, "unaccepted subscription", http.StatusBadRequest)
		return
	}

	updated, err := applyWebhookSubscription(r.Context(), h.db, result, "webhook", string(body), "")
	if err != nil {
		slog.Error("google webhook: applying subscription", "error", err)
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	slog.Info("google webhook processed", "purchase_token", purchaseToken, "updated_current", updated)

	w.WriteHeader(http.StatusOK)
}

// appleNotificationHasTransaction reports whether a notification carries a
// signed transaction to act on. TEST notifications and the external-purchase
// types do not.
func appleNotificationHasTransaction(n *AppleNotificationPayload) bool {
	return n != nil && n.Data.SignedTransactionInfo != ""
}
