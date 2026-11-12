package iap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jmoiron/sqlx"
)

// Handler handles IAP validation endpoints called by the mobile app.
type Handler struct {
	db      *sqlx.DB
	apple   *AppleClient
	google  *GoogleClient
	options Options
}

func NewHandler(db *sqlx.DB, apple *AppleClient, google *GoogleClient, options ...Options) *Handler {
	var option Options
	if len(options) > 0 {
		option = options[0]
	}
	return &Handler{db: db, apple: apple, google: google, options: option}
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
		result, err = h.apple.GetSubscriptionStatus(req.TransactionID, "")
	} else if req.TransactionID != "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "send jws_transaction for validation"})
		return
	} else {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "jws_transaction or transaction_id required"})
		return
	}

	if err != nil {
		slog.Error("apple validation failed", "error", err, "user_id", userID)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "validation failed"})
		return
	}

	if result.Valid {
		if err := h.options.validate(result); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "validation failed: " + err.Error()})
			return
		}
		if err := applySubscription(r.Context(), h.db, userID, result, "purchase", req.JWSTransaction); err != nil {
			writeSubscriptionError(w, err)
			return
		}
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

	// The signed transaction proves the purchase but says nothing about
	// renewal. With API credentials, ask Apple for the subscription's current
	// state (auto-renew, grace, billing retry); fall back to the transaction
	// alone if the API is unavailable.
	// Apple knowing nothing about the transaction is a rejection, not an
	// outage: only transport and server errors fall back to the JWS alone.
	if h.apple != nil {
		status, err := h.apple.GetSubscriptionStatus(txn.OriginalTransactionID, txn.Environment)
		if err == nil && status.OriginalTransactionID == txn.OriginalTransactionID {
			return status, nil
		}
		if IsAppleNotFound(err) {
			return nil, fmt.Errorf("Apple has no record of transaction %s", txn.OriginalTransactionID)
		}
		slog.Warn("apple: subscription status unavailable, using the signed transaction alone",
			"original_transaction_id", txn.OriginalTransactionID, "error", err)
	}
	return appleTransactionResult(txn), nil
}

// appleTransactionResult is shared by the preferred verified-JWS path's
// post-verification mapping. Store identity and environment must be preserved
// because Options validates them before granting access.
func appleTransactionResult(txn *AppleJWSTransactionPayload) *ValidationResult {
	expiresAt := time.UnixMilli(txn.ExpiresDate)
	return &ValidationResult{
		Valid:                 expiresAt.After(time.Now()),
		ProductID:             txn.ProductID,
		Platform:              "apple",
		Environment:           txn.Environment,
		ExpiresAt:             &expiresAt,
		TransactionID:         txn.TransactionID,
		OriginalTransactionID: txn.OriginalTransactionID,
		// Unknown without renewal info; assume the common case. Only used when
		// the App Store Server API cannot be reached.
		WillRenew: true,
	}
}

// ValidateGoogle handles POST /iap/google/validate
func (h *Handler) ValidateGoogle(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r.Context())
	if userID == 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req GoogleValidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PurchaseToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "purchase_token required"})
		return
	}

	if h.google == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Google IAP not configured"})
		return
	}

	result, err := h.google.ValidatePurchase(r.Context(), req.PurchaseToken, req.ProductID)
	if err != nil {
		slog.Error("google validation failed", "error", err, "user_id", userID)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "validation failed"})
		return
	}

	if result.Valid {
		if err := h.options.validate(result); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "validation failed: " + err.Error()})
			return
		}
		if err := applySubscription(r.Context(), h.db, userID, result, "purchase", req.PurchaseToken); err != nil {
			writeSubscriptionError(w, err)
			return
		}
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
			if result.Valid && h.options.validate(result) == nil && (bestResult == nil || laterThan(result, bestResult)) {
				bestResult = result
			}
		}
		// Fallback to transaction IDs via Apple API
		if bestResult == nil && h.apple != nil {
			for _, txnID := range req.TransactionIDs {
				result, err := h.apple.GetSubscriptionStatus(txnID, "")
				if err != nil {
					slog.Error("restore: apple API failed", "error", err, "txn_id", txnID)
					continue
				}
				if result.Valid && h.options.validate(result) == nil && (bestResult == nil || laterThan(result, bestResult)) {
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
			if result.Valid && h.options.validate(result) == nil {
				if bestResult == nil || laterThan(result, bestResult) {
					bestResult = result
				}
			}
		}
	}

	if bestResult != nil {
		if err := applySubscription(r.Context(), h.db, userID, bestResult, "restore", ""); err != nil {
			writeSubscriptionError(w, err)
			return
		}
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

func laterThan(candidate, current *ValidationResult) bool {
	if candidate.ExpiresAt == nil {
		return false
	}
	return current.ExpiresAt == nil || candidate.ExpiresAt.After(*current.ExpiresAt)
}

func writeSubscriptionError(w http.ResponseWriter, err error) {
	var conflict *OwnershipConflictError
	if errors.As(err, &conflict) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "subscription is already linked to another account"})
		return
	}
	slog.Error("subscription update failed", "error", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "subscription update failed"})
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
