package iap

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// AdaptyWebhookHandler handles POST /api/adapty-webhook — the same path the
// Laravel app served, so the Adapty dashboard needs no change at cutover.
//
// The old app still buys through Adapty, so this is what keeps those users'
// entitlement current. Unlike the Laravel handler it also records the store
// identifiers (Apple original transaction ID, Google purchase token) and the
// ownership link, so Go's direct store webhooks and /iap/restore recognise the
// purchase without a separate Adapty backfill.
type AdaptyWebhookHandler struct {
	db         *sqlx.DB
	authHeader string // optional; when set, Authorization must match exactly
}

func NewAdaptyWebhookHandler(db *sqlx.DB, authHeader string) *AdaptyWebhookHandler {
	if authHeader == "" {
		slog.Warn("adapty webhook: ADAPTY_WEBHOOK_AUTH not set, accepting unauthenticated events")
	}
	return &AdaptyWebhookHandler{db: db, authHeader: authHeader}
}

const adaptyMaxBody = 1 << 20

type adaptyEvent struct {
	AdaptyCheck            *string               `json:"adapty_check"`
	ProfileID              string                `json:"profile_id"`
	CustomerUserID         *string               `json:"customer_user_id"`
	EventType              string                `json:"event_type"`
	EventProperties        adaptyEventProperties `json:"event_properties"`
	PlayStorePurchaseToken *adaptyPlayStoreToken `json:"play_store_purchase_token"`
}

type adaptyEventProperties struct {
	Store                       string  `json:"store"`
	Environment                 string  `json:"environment"`
	VendorProductID             string  `json:"vendor_product_id"`
	TransactionID               string  `json:"transaction_id"`
	OriginalTransactionID       string  `json:"original_transaction_id"`
	VendorTransactionID         string  `json:"vendor_transaction_id"`
	VendorOriginalTransactionID string  `json:"vendor_original_transaction_id"`
	SubscriptionExpiresAt       string  `json:"subscription_expires_at"`
	ExpiresAt                   string  `json:"expires_at"`
	IsActive                    *bool   `json:"is_active"`
	IsInGracePeriod             *bool   `json:"is_in_grace_period"`
	IsRefund                    *bool   `json:"is_refund"`
	WillRenew                   *bool   `json:"will_renew"`
	CancellationReason          *string `json:"cancellation_reason"`
}

type adaptyPlayStoreToken struct {
	PurchaseToken string `json:"purchase_token"`
	ProductID     string `json:"product_id"`
}

// adaptyUpdate is the user-row change derived from one event.
type adaptyUpdate struct {
	CustomerUserID     string
	ProfileID          string
	EventType          string
	HasSubscription    bool   // false → only touch adapty_id / last_adapty_sync_at
	Platform           string // "apple", "google" or "" (unknown store: leave as is)
	ProductID          string
	Expiry             time.Time
	Valid              bool
	WillRenew          *bool
	CancellationReason string
	StoreOriginalID    string // Apple original transaction ID or Google purchase token
	StoreTransactionID string
	Environment        string
}

// adaptyTimeLayouts covers Adapty's "2026-09-24T05:46:18.000000+0000" format
// and plain RFC 3339.
var adaptyTimeLayouts = []string{
	"2006-01-02T15:04:05.999999-0700",
	"2006-01-02T15:04:05-0700",
	time.RFC3339Nano,
}

func parseAdaptyTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range adaptyTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// deriveAdaptyUpdate turns an event into a user-row update. now is injected
// for tests.
func deriveAdaptyUpdate(ev *adaptyEvent, now time.Time) adaptyUpdate {
	p := ev.EventProperties
	u := adaptyUpdate{
		ProfileID:   ev.ProfileID,
		EventType:   ev.EventType,
		ProductID:   p.VendorProductID,
		WillRenew:   p.WillRenew,
		Environment: p.Environment,
	}
	if ev.CustomerUserID != nil {
		u.CustomerUserID = strings.TrimSpace(*ev.CustomerUserID)
	}
	if p.CancellationReason != nil {
		u.CancellationReason = *p.CancellationReason
	}

	switch strings.ToLower(p.Store) {
	case "app_store":
		u.Platform = "apple"
		u.StoreOriginalID = firstNonEmpty(p.OriginalTransactionID, p.VendorOriginalTransactionID)
		u.StoreTransactionID = firstNonEmpty(p.TransactionID, p.VendorTransactionID)
	case "play_store":
		u.Platform = "google"
		// Adapty's original_transaction_id is the Play *order* ID (GPA.…);
		// RTDN and the Play API key on the purchase token instead.
		if ev.PlayStorePurchaseToken != nil {
			u.StoreOriginalID = strings.TrimSpace(ev.PlayStorePurchaseToken.PurchaseToken)
		}
		u.StoreTransactionID = firstNonEmpty(p.TransactionID, p.VendorTransactionID)
	}

	expiry, ok := parseAdaptyTime(firstNonEmpty(p.SubscriptionExpiresAt, p.ExpiresAt))
	if !ok {
		return u
	}
	u.HasSubscription = true
	u.Expiry = expiry

	switch {
	case (p.IsRefund != nil && *p.IsRefund) || strings.Contains(ev.EventType, "refund"):
		u.Valid = false
		if u.CancellationReason == "" {
			u.CancellationReason = "refund"
		}
	case p.IsActive != nil:
		// access_level_updated carries the authoritative access state,
		// including grace periods where the expiry has already passed.
		u.Valid = *p.IsActive
	default:
		u.Valid = expiry.After(now)
	}
	return u
}

func (h *AdaptyWebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if h.authHeader != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(h.authHeader)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, adaptyMaxBody))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	var ev adaptyEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		slog.Warn("adapty webhook: invalid JSON", "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}

	// Adapty's endpoint verification handshake.
	if ev.AdaptyCheck != nil {
		writeJSON(w, http.StatusOK, map[string]string{"adapty_check_response": *ev.AdaptyCheck, "status": "ok"})
		return
	}

	u := deriveAdaptyUpdate(&ev, time.Now())
	if _, err := uuid.Parse(u.CustomerUserID); err != nil {
		slog.Info("adapty webhook: no matching customer_user_id", "event", u.EventType, "profile_id", u.ProfileID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	userID, err := h.apply(r.Context(), u, string(body))
	if errors.Is(err, sql.ErrNoRows) {
		slog.Info("adapty webhook: user not found", "event", u.EventType, "customer_user_id", u.CustomerUserID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if err != nil {
		// 5xx so Adapty retries.
		slog.Error("adapty webhook: apply failed", "event", u.EventType, "customer_user_id", u.CustomerUserID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "err"})
		return
	}

	slog.Info("adapty webhook applied", "event", u.EventType, "user_id", userID, "platform", u.Platform,
		"valid", u.Valid, "expiry", u.Expiry, "store_id", u.StoreOriginalID != "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *AdaptyWebhookHandler) apply(ctx context.Context, u adaptyUpdate, raw string) (int64, error) {
	tx, err := h.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var userID int64
	if !u.HasSubscription {
		err = tx.GetContext(ctx, &userID, `
			UPDATE users SET
				adapty_id           = COALESCE(NULLIF($1::text, ''), adapty_id),
				last_adapty_sync_at = NOW(),
				updated_at          = NOW()
			 WHERE user_id = $2::uuid
			 RETURNING id`, u.ProfileID, u.CustomerUserID)
		if err != nil {
			return 0, err
		}
		return userID, tx.Commit()
	}

	// $4 and $8 appear both as assigned values and inside CASE comparisons
	// against columns of differing types; the explicit casts stop Postgres
	// rejecting the statement with 42P08.
	err = tx.GetContext(ctx, &userID, `
		UPDATE users SET
			subscription_valid      = $1,
			subscription_expiry     = $2,
			subscription_product_id = COALESCE(NULLIF($3::text, ''), subscription_product_id),
			subscription_platform   = COALESCE(NULLIF($4::text, ''), subscription_platform),
			will_renew              = COALESCE($5, will_renew),
			cancellation_reason     = COALESCE(NULLIF($6::text, ''), cancellation_reason),
			adapty_id               = COALESCE(NULLIF($7::text, ''), adapty_id),
			apple_original_transaction_id = CASE WHEN $4::text = 'apple'  AND $8::text <> '' THEN $8::text ELSE apple_original_transaction_id END,
			google_purchase_token         = CASE WHEN $4::text = 'google' AND $8::text <> '' THEN $8::text ELSE google_purchase_token END,
			last_adapty_sync_at     = NOW(),
			updated_at              = NOW()
		 WHERE user_id = $9::uuid
		 RETURNING id`,
		u.Valid, u.Expiry, u.ProductID, u.Platform, u.WillRenew, u.CancellationReason,
		u.ProfileID, u.StoreOriginalID, u.CustomerUserID,
	)
	if err != nil {
		return 0, err
	}

	if u.StoreOriginalID != "" && (u.Platform == "apple" || u.Platform == "google") {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO iap_subscription_ownership (platform, original_transaction_id, user_id, updated_at)
			VALUES ($1, $2, $3, NOW())
			ON CONFLICT (platform, original_transaction_id, user_id) DO UPDATE SET updated_at = NOW()`,
			u.Platform, u.StoreOriginalID, userID,
		); err != nil {
			return 0, fmt.Errorf("link iap ownership: %w", err)
		}
	}

	platform := u.Platform
	if platform == "" {
		platform = "adapty"
	}
	productID := u.ProductID
	if productID == "" {
		productID = "unknown"
	}
	eventType := "adapty:" + u.EventType
	if len(eventType) > 50 {
		eventType = eventType[:50]
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO iap_transactions (
			user_id, platform, transaction_id, original_transaction_id, product_id,
			event_type, is_valid, expires_at, will_renew, environment, raw_payload
		) VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5, $6, $7, $8, $9, NULLIF($10, ''), $11)`,
		userID, platform, u.StoreTransactionID, u.StoreOriginalID, productID,
		eventType, u.Valid, u.Expiry, u.WillRenew, u.Environment, raw,
	); err != nil {
		return 0, fmt.Errorf("insert iap_transactions: %w", err)
	}

	return userID, tx.Commit()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
