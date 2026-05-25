package iap

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jmoiron/sqlx"
)

// Handler handles IAP validation endpoints called by the mobile app.
type Handler struct {
	db     *sqlx.DB
	apple  *AppleClient
	google *GoogleClient
}

func NewHandler(db *sqlx.DB, apple *AppleClient, google *GoogleClient) *Handler {
	return &Handler{db: db, apple: apple, google: google}
}

// ValidateApple handles POST /iap/apple/validate
// Accepts either a JWS signed transaction (preferred, no Apple API key needed)
// or a transaction_id (requires Apple API credentials).
func (h *Handler) ValidateApple(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r.Context())
	if userID == 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req AppleValidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	slog.Info("apple validate request", "user_id", userID,
		"has_jws", req.JWSTransaction != "",
		"txn_id", req.TransactionID)

	var result *ValidationResult
	var err error

	if req.JWSTransaction != "" {
		// Preferred: decode JWS signed transaction locally (no Apple API needed)
		result, err = h.validateAppleJWS(req.JWSTransaction)
	} else if req.TransactionID != "" && h.apple != nil {
		// Fallback: look up via Apple's App Store Server API (needs credentials)
		result, err = h.apple.GetTransactionInfo(req.TransactionID)
	} else if req.TransactionID != "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "send jws_transaction for validation"})
		return
	} else {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "jws_transaction or transaction_id required"})
		return
	}

	if err != nil {
		slog.Error("apple validation failed", "error", err, "user_id", userID)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "validation failed: " + err.Error()})
		return
	}

	if result.Valid {
		h.updateSubscription(r.Context(), userID, result, "purchase", req.JWSTransaction)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"valid":      result.Valid,
		"product_id": result.ProductID,
		"expires_at": result.ExpiresAt,
		"will_renew": result.WillRenew,
	})
}

// validateAppleJWS decodes and validates an Apple JWS signed transaction locally.
func (h *Handler) validateAppleJWS(jwsTransaction string) (*ValidationResult, error) {
	txn, err := DecodeAppleJWS(jwsTransaction)
	if err != nil {
		return nil, fmt.Errorf("decoding JWS: %w", err)
	}

	// Check bundle ID
	bundleID := "io.devlan.rfspager"
	if h.apple != nil {
		bundleID = h.apple.bundleID
	}
	if txn.BundleID != bundleID {
		return nil, fmt.Errorf("bundle ID mismatch: got %s, want %s", txn.BundleID, bundleID)
	}

	// Check revocation
	if txn.RevocationDate != nil {
		return &ValidationResult{Valid: false, Platform: "apple"}, nil
	}

	expiresAt := time.UnixMilli(txn.ExpiresDate)
	return &ValidationResult{
		Valid:                 expiresAt.After(time.Now()),
		ProductID:             txn.ProductID,
		Platform:              "apple",
		ExpiresAt:             &expiresAt,
		TransactionID:         txn.TransactionID,
		OriginalTransactionID: txn.OriginalTransactionID,
		WillRenew:             true,
	}, nil
}

// ValidateGoogle handles POST /iap/google/validate
func (h *Handler) ValidateGoogle(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r.Context())
	if userID == 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req GoogleValidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PurchaseToken == "" || req.ProductID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "purchase_token and product_id required"})
		return
	}

	if h.google == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Google IAP not configured"})
		return
	}

	result, err := h.google.ValidatePurchase(r.Context(), req.PurchaseToken, req.ProductID)
	if err != nil {
		slog.Error("google validation failed", "error", err, "user_id", userID)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "validation failed: " + err.Error()})
		return
	}

	if result.Valid {
		// Store the purchase token for webhook lookups
		h.db.ExecContext(r.Context(),
			`UPDATE users SET google_purchase_token = $1 WHERE id = $2`,
			req.PurchaseToken, userID)
		h.updateSubscription(r.Context(), userID, result, "purchase", req.PurchaseToken)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"valid":      result.Valid,
		"product_id": result.ProductID,
		"expires_at": result.ExpiresAt,
		"will_renew": result.WillRenew,
	})
}

// Restore handles POST /iap/restore
func (h *Handler) Restore(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r.Context())
	if userID == 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req RestoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	var bestResult *ValidationResult

	if req.Platform == "ios" {
		// Prefer JWS transactions (no Apple API needed)
		for _, jws := range req.JWSTransactions {
			result, err := h.validateAppleJWS(jws)
			if err != nil {
				slog.Error("restore: JWS validation failed", "error", err)
				continue
			}
			if result.Valid && (bestResult == nil || result.ExpiresAt.After(*bestResult.ExpiresAt)) {
				bestResult = result
			}
		}
		// Fallback to transaction IDs via Apple API
		if bestResult == nil && h.apple != nil {
			for _, txnID := range req.TransactionIDs {
				result, err := h.apple.GetTransactionInfo(txnID)
				if err != nil {
					slog.Error("restore: apple API failed", "error", err, "txn_id", txnID)
					continue
				}
				if result.Valid && (bestResult == nil || result.ExpiresAt.After(*bestResult.ExpiresAt)) {
					bestResult = result
				}
			}
		}
	} else if req.Platform == "android" && h.google != nil {
		for _, purchase := range req.Purchases {
			result, err := h.google.ValidatePurchase(r.Context(), purchase.PurchaseToken, purchase.ProductID)
			if err != nil {
				slog.Error("restore: google validation failed", "error", err)
				continue
			}
			if result.Valid {
				h.db.ExecContext(r.Context(),
					`UPDATE users SET google_purchase_token = $1 WHERE id = $2`,
					purchase.PurchaseToken, userID)
				if bestResult == nil || result.ExpiresAt.After(*bestResult.ExpiresAt) {
					bestResult = result
				}
			}
		}
	}

	if bestResult != nil {
		h.updateSubscription(r.Context(), userID, bestResult, "restore", "")
		writeJSON(w, http.StatusOK, map[string]any{
			"restored":   true,
			"product_id": bestResult.ProductID,
			"expires_at": bestResult.ExpiresAt,
			"will_renew": bestResult.WillRenew,
		})
	} else {
		writeJSON(w, http.StatusOK, map[string]any{
			"restored": false,
			"message":  "no active subscriptions found",
		})
	}
}

func (h *Handler) updateSubscription(ctx context.Context, userID int64, result *ValidationResult, eventType string, rawPayload string) {
	var expiresAt *time.Time
	if result.ExpiresAt != nil {
		expiresAt = result.ExpiresAt
	}

	_, err := h.db.ExecContext(ctx,
		`UPDATE users SET
			subscription_valid = $1,
			subscription_expiry = $2,
			subscription_product_id = $3,
			subscription_platform = $4,
			will_renew = $5,
			apple_original_transaction_id = CASE WHEN $6 = 'apple' THEN $7 ELSE apple_original_transaction_id END,
			last_store_sync_at = NOW(),
			updated_at = NOW()
		 WHERE id = $8`,
		result.Valid, expiresAt, result.ProductID, result.Platform,
		result.WillRenew, result.Platform, result.OriginalTransactionID, userID)
	if err != nil {
		slog.Error("failed to update subscription", "error", err, "user_id", userID)
	}

	// Append to transaction log
	h.logTransaction(ctx, userID, result, eventType, rawPayload)
}

func (h *Handler) logTransaction(ctx context.Context, userID int64, result *ValidationResult, eventType string, rawPayload string) {
	_, err := h.db.ExecContext(ctx,
		`INSERT INTO iap_transactions (user_id, platform, transaction_id, original_transaction_id, product_id, event_type, is_valid, expires_at, will_renew, raw_payload)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		userID, result.Platform, result.TransactionID, result.OriginalTransactionID,
		result.ProductID, eventType, result.Valid, result.ExpiresAt, result.WillRenew,
		rawPayload)
	if err != nil {
		slog.Error("failed to log iap transaction", "error", err, "user_id", userID)
	}
}

type ctxKey string

const UserIDKey ctxKey = "iap_user_id"

// WithUserID sets the user ID in context for IAP handlers.
func WithUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, UserIDKey, userID)
}

func getUserID(ctx context.Context) int64 {
	id, _ := ctx.Value(UserIDKey).(int64)
	return id
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
