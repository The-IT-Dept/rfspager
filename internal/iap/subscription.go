package iap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// OwnershipConflictError means a verified store entitlement is already bound
// to another RFSPager account. It is deliberately safe to expose to the app:
// it does not reveal the account that owns the entitlement.
type OwnershipConflictError struct {
	Platform   string
	Identifier string
}

func (e *OwnershipConflictError) Error() string {
	return fmt.Sprintf("%s subscription is already linked to another account", e.Platform)
}

// applySubscription records every store identifier in the ownership ledger
// before changing the user's current subscription pointer. A user can acquire
// a later, distinct entitlement, while old receipts remain permanently bound
// and cannot be replayed by another account.
func applySubscription(ctx context.Context, db *sqlx.DB, userID int64, result *ValidationResult, eventType, rawPayload string) error {
	if result == nil {
		return errors.New("missing subscription result")
	}
	column, err := ownershipColumn(result.Platform)
	if err != nil {
		return err
	}
	if result.OriginalTransactionID == "" {
		return errors.New("missing store ownership identifier")
	}

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting subscription update: %w", err)
	}
	defer tx.Rollback()

	if err := claimOwnership(ctx, tx, result.Platform, result.OriginalTransactionID, userID); err != nil {
		return err
	}
	if result.Platform == "google" && result.PreviousOwnershipID != "" && result.PreviousOwnershipID != result.OriginalTransactionID {
		if err := claimOwnership(ctx, tx, result.Platform, result.PreviousOwnershipID, userID); err != nil {
			return err
		}
	}

	var expiresAt *time.Time
	if result.ExpiresAt != nil {
		expiresAt = result.ExpiresAt
	}
	query := fmt.Sprintf(`
		UPDATE users SET
			subscription_valid = $1,
			subscription_expiry = $2,
			subscription_product_id = $3,
			subscription_platform = $4,
			will_renew = $5,
			%s = $6,
			last_store_sync_at = NOW(),
			updated_at = NOW()
		WHERE id = $7`, column)
	update, err := tx.ExecContext(ctx, query,
		result.Valid, expiresAt, result.ProductID, result.Platform, result.WillRenew,
		result.OriginalTransactionID, userID)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return &OwnershipConflictError{Platform: result.Platform, Identifier: result.OriginalTransactionID}
		}
		return fmt.Errorf("updating %s subscription: %w", result.Platform, err)
	}
	rows, err := update.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking subscription update: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("user %d does not exist", userID)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO iap_transactions (user_id, platform, transaction_id, original_transaction_id, product_id, event_type, is_valid, expires_at, will_renew, environment, raw_payload)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		userID, result.Platform, result.TransactionID, result.OriginalTransactionID,
		result.ProductID, eventType, result.Valid, expiresAt, result.WillRenew,
		result.Environment, rawPayload); err != nil {
		return fmt.Errorf("recording subscription transaction: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing subscription update: %w", err)
	}
	return nil
}

func claimOwnership(ctx context.Context, tx *sqlx.Tx, platform, identifier string, userID int64) error {
	var ownerID *int64
	err := tx.GetContext(ctx, &ownerID, `
		INSERT INTO iap_subscription_ownership
			(platform, original_transaction_id, user_id, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (platform, original_transaction_id) DO UPDATE
			SET updated_at = NOW()
			WHERE iap_subscription_ownership.user_id = EXCLUDED.user_id
		RETURNING user_id`, platform, identifier, userID)
	if err == nil && ownerID != nil && *ownerID == userID {
		return nil
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &OwnershipConflictError{Platform: platform, Identifier: identifier}
		}
		return fmt.Errorf("claiming %s subscription ownership: %w", platform, err)
	}
	return &OwnershipConflictError{Platform: platform, Identifier: identifier}
}

// applyWebhookSubscription only changes a user's subscription when the event
// names that user's current store identifier. Historical events are still
// recorded against their ledger owner, but cannot downgrade a newer purchase.
// A Google token newly linked to a known historical token is claimed for that
// same owner and may advance the current pointer.
func applyWebhookSubscription(ctx context.Context, db *sqlx.DB, result *ValidationResult, eventType, rawPayload, cancellationReason string) (bool, error) {
	if result == nil || result.OriginalTransactionID == "" {
		return false, errors.New("missing webhook ownership identifier")
	}
	if _, err := ownershipColumn(result.Platform); err != nil {
		return false, err
	}
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("starting webhook update: %w", err)
	}
	defer tx.Rollback()

	ownerID, err := ledgerOwner(ctx, tx, result.Platform, result.OriginalTransactionID)
	if err != nil {
		return false, err
	}
	activeIdentifier := result.OriginalTransactionID
	if ownerID == nil && result.Platform == "google" && result.PreviousOwnershipID != "" {
		ownerID, err = ledgerOwner(ctx, tx, result.Platform, result.PreviousOwnershipID)
		if err != nil {
			return false, err
		}
		if ownerID != nil {
			if err := claimOwnership(ctx, tx, result.Platform, result.OriginalTransactionID, *ownerID); err != nil {
				return false, err
			}
			activeIdentifier = result.PreviousOwnershipID
		}
	}
	if ownerID == nil {
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("committing unowned webhook: %w", err)
		}
		return false, nil
	}

	var expiresAt *time.Time
	if result.ExpiresAt != nil {
		expiresAt = result.ExpiresAt
	}
	var updateResult sql.Result
	if result.Platform == "apple" {
		updateResult, err = tx.ExecContext(ctx, `
			UPDATE users SET subscription_valid = $1, subscription_expiry = $2,
				subscription_product_id = $3, subscription_platform = 'apple', will_renew = $4,
				cancellation_reason = CASE WHEN $5 = '' THEN cancellation_reason ELSE $5 END,
				last_store_sync_at = NOW(), updated_at = NOW()
			WHERE id = $6 AND subscription_platform = 'apple' AND apple_original_transaction_id = $7`,
			result.Valid, expiresAt, result.ProductID, result.WillRenew, cancellationReason,
			*ownerID, activeIdentifier)
	} else {
		updateResult, err = tx.ExecContext(ctx, `
			UPDATE users SET subscription_valid = $1, subscription_expiry = $2,
				subscription_product_id = $3, subscription_platform = 'google', will_renew = $4,
				google_purchase_token = $5, last_store_sync_at = NOW(), updated_at = NOW()
			WHERE id = $6 AND subscription_platform = 'google' AND google_purchase_token = $7`,
			result.Valid, expiresAt, result.ProductID, result.WillRenew,
			result.OriginalTransactionID, *ownerID, activeIdentifier)
	}
	if err != nil {
		return false, fmt.Errorf("updating webhook subscription: %w", err)
	}
	updatedRows, err := updateResult.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("checking webhook update: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO iap_transactions (user_id, platform, transaction_id, original_transaction_id, product_id, event_type, is_valid, expires_at, will_renew, environment, raw_payload)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		*ownerID, result.Platform, result.TransactionID, result.OriginalTransactionID,
		result.ProductID, eventType, result.Valid, expiresAt, result.WillRenew,
		result.Environment, rawPayload); err != nil {
		return false, fmt.Errorf("recording webhook transaction: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("committing webhook update: %w", err)
	}
	return updatedRows == 1, nil
}

func ledgerOwner(ctx context.Context, tx *sqlx.Tx, platform, identifier string) (*int64, error) {
	var ownerID *int64
	err := tx.GetContext(ctx, &ownerID,
		`SELECT user_id FROM iap_subscription_ownership WHERE platform = $1 AND original_transaction_id = $2`,
		platform, identifier)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("looking up %s subscription owner: %w", platform, err)
	}
	return ownerID, nil
}

func ownershipColumn(platform string) (string, error) {
	switch platform {
	case "apple":
		return "apple_original_transaction_id", nil
	case "google":
		return "google_purchase_token", nil
	default:
		return "", fmt.Errorf("unsupported IAP platform %q", platform)
	}
}
