package iap

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// OwnershipConflictError is retained for callers/tests even though a verified
// store entitlement may now be shared by several accounts. It is deliberately
// safe to expose to the app: it does not reveal the account that owns the
// entitlement.
type OwnershipConflictError struct {
	Platform   string
	Identifier string
}

func (e *OwnershipConflictError) Error() string {
	return fmt.Sprintf("%s subscription is already linked to another account", e.Platform)
}

// applySubscription records the store identifier in the shared entitlement
// ledger and points the user's current subscription at it. A second account
// restoring the same identifier simply links to it as well; there is no
// conflict. A user can also acquire a later, distinct entitlement while old
// receipts stay linked for webhook fan-out.
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

	if err := linkEntitlement(ctx, tx, result.Platform, result.OriginalTransactionID, userID); err != nil {
		return err
	}
	// A Google upgrade supersedes a previous token; link the caller to it too so
	// the upgrade carries their entitlement across.
	if result.Platform == "google" && result.PreviousOwnershipID != "" && result.PreviousOwnershipID != result.OriginalTransactionID {
		if err := linkEntitlement(ctx, tx, result.Platform, result.PreviousOwnershipID, userID); err != nil {
			return err
		}
	}

	var expiresAt *time.Time
	if result.ExpiresAt != nil {
		expiresAt = result.ExpiresAt
	}
	// A store validation only speaks for its own subscription. When the
	// account already holds access running past this result's expiry (a gift,
	// a comp, a purchase on the other platform), the subscription columns are
	// left alone: the store webhooks apply the same rule, and the
	// app re-validating its purchase must never shorten access granted
	// elsewhere. The store identity and the audit row are still recorded.
	query := fmt.Sprintf(`
		WITH cur AS (
			SELECT id, (subscription_valid AND (subscription_expiry IS NULL OR subscription_expiry > $2::timestamptz)) AS keep
			  FROM users WHERE id = $7 FOR UPDATE
		)
		UPDATE users u SET
			subscription_valid      = CASE WHEN cur.keep THEN u.subscription_valid ELSE $1 END,
			subscription_expiry     = CASE WHEN cur.keep THEN u.subscription_expiry ELSE $2::timestamptz END,
			subscription_product_id = CASE WHEN cur.keep THEN u.subscription_product_id ELSE $3 END,
			subscription_platform   = CASE WHEN cur.keep THEN u.subscription_platform ELSE $4 END,
			will_renew              = CASE WHEN cur.keep THEN u.will_renew ELSE $5 END,
			%s = $6,
			last_store_sync_at = NOW(),
			updated_at = NOW()
		FROM cur
		WHERE u.id = cur.id`, column)
	update, err := tx.ExecContext(ctx, query,
		result.Valid, expiresAt, result.ProductID, result.Platform, result.WillRenew,
		result.OriginalTransactionID, userID)
	if err != nil {
		return fmt.Errorf("updating %s subscription: %w", result.Platform, err)
	}
	rows, err := update.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking subscription update: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("user %d does not exist", userID)
	}

	if err := recordTransaction(ctx, tx, userID, result, eventType, rawPayload); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing subscription update: %w", err)
	}
	return nil
}

// linkEntitlement links a store identifier to an account. Idempotent: a repeat
// restore or a re-run backfill only bumps updated_at.
func linkEntitlement(ctx context.Context, tx *sqlx.Tx, platform, identifier string, userID int64) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO iap_subscription_ownership
			(platform, original_transaction_id, user_id, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (platform, original_transaction_id, user_id)
			DO UPDATE SET updated_at = NOW()`,
		platform, identifier, userID); err != nil {
		return fmt.Errorf("linking %s entitlement: %w", platform, err)
	}
	return nil
}

// entitlementUsers returns every account currently linked to an identifier.
func entitlementUsers(ctx context.Context, tx *sqlx.Tx, platform, identifier string) ([]int64, error) {
	var ids []int64
	if err := tx.SelectContext(ctx, &ids,
		`SELECT user_id FROM iap_subscription_ownership
		 WHERE platform = $1 AND original_transaction_id = $2`,
		platform, identifier); err != nil {
		return nil, fmt.Errorf("looking up %s entitlement users: %w", platform, err)
	}
	return ids, nil
}

// applyWebhookSubscription fans a store event out to every account the
// identifier entitles, but only changes an account whose current pointer is
// that identifier (or the Google token being upgraded). Historical events are
// still recorded against the linked accounts without clobbering a newer
// purchase. Returns whether any account's current subscription changed.
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

	users, err := entitlementUsers(ctx, tx, result.Platform, result.OriginalTransactionID)
	if err != nil {
		return false, err
	}
	activeIdentifier := result.OriginalTransactionID
	identifiers := []string{activeIdentifier}
	if result.Platform == "google" && result.PreviousOwnershipID != "" && result.PreviousOwnershipID != activeIdentifier {
		prevUsers, err := entitlementUsers(ctx, tx, result.Platform, result.PreviousOwnershipID)
		if err != nil {
			return false, err
		}
		for _, u := range prevUsers {
			if err := linkEntitlement(ctx, tx, result.Platform, activeIdentifier, u); err != nil {
				return false, err
			}
		}
		users = mergeIDs(users, prevUsers)
		identifiers = append(identifiers, result.PreviousOwnershipID)
	}
	if len(users) == 0 {
		// An event for a transaction we have never linked. The user is repaired
		// by the next /iap/restore.
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("committing unowned webhook: %w", err)
		}
		return false, nil
	}

	var expiresAt *time.Time
	if result.ExpiresAt != nil {
		expiresAt = result.ExpiresAt
	}

	var affected []int64
	if result.Platform == "apple" {
		affected, err = updateAppleWebhook(ctx, tx, result, expiresAt, cancellationReason, users, activeIdentifier)
	} else {
		affected, err = updateGoogleWebhook(ctx, tx, result, expiresAt, users, identifiers, activeIdentifier)
	}
	if err != nil {
		return false, err
	}

	audit := affected
	if len(audit) == 0 {
		// Historical event: record it against the link so it is not lost, but
		// leave the newer current purchase untouched.
		audit = users
	}
	for _, userID := range audit {
		if err := recordTransaction(ctx, tx, userID, result, eventType, rawPayload); err != nil {
			return false, err
		}
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("committing webhook update: %w", err)
	}
	return len(affected) > 0, nil
}

func updateAppleWebhook(ctx context.Context, tx *sqlx.Tx, result *ValidationResult, expiresAt *time.Time, cancellationReason string, users []int64, activeIdentifier string) ([]int64, error) {
	var affected []int64
	err := tx.SelectContext(ctx, &affected, `
		UPDATE users SET subscription_valid = $1, subscription_expiry = $2,
			subscription_product_id = $3, subscription_platform = 'apple', will_renew = $4,
			cancellation_reason = CASE WHEN $5 = '' THEN cancellation_reason ELSE $5 END,
			last_store_sync_at = NOW(), updated_at = NOW()
		WHERE id = ANY($6) AND subscription_platform = 'apple' AND apple_original_transaction_id = $7
		RETURNING id`,
		result.Valid, expiresAt, result.ProductID, result.WillRenew, cancellationReason,
		pq.Array(users), activeIdentifier)
	if err != nil {
		return nil, fmt.Errorf("updating apple webhook subscription: %w", err)
	}
	return affected, nil
}

func updateGoogleWebhook(ctx context.Context, tx *sqlx.Tx, result *ValidationResult, expiresAt *time.Time, users []int64, identifiers []string, activeIdentifier string) ([]int64, error) {
	var affected []int64
	err := tx.SelectContext(ctx, &affected, `
		UPDATE users SET subscription_valid = $1, subscription_expiry = $2,
			subscription_product_id = $3, subscription_platform = 'google', will_renew = $4,
			google_purchase_token = $5, last_store_sync_at = NOW(), updated_at = NOW()
		WHERE id = ANY($6) AND subscription_platform = 'google' AND google_purchase_token = ANY($7)
		RETURNING id`,
		result.Valid, expiresAt, result.ProductID, result.WillRenew,
		activeIdentifier, pq.Array(users), pq.Array(identifiers))
	if err != nil {
		return nil, fmt.Errorf("updating google webhook subscription: %w", err)
	}
	return affected, nil
}

func recordTransaction(ctx context.Context, tx *sqlx.Tx, userID int64, result *ValidationResult, eventType, rawPayload string) error {
	var expiresAt *time.Time
	if result.ExpiresAt != nil {
		expiresAt = result.ExpiresAt
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO iap_transactions (user_id, platform, transaction_id, original_transaction_id, product_id, event_type, is_valid, expires_at, will_renew, environment, raw_payload)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		userID, result.Platform, result.TransactionID, result.OriginalTransactionID,
		result.ProductID, eventType, result.Valid, expiresAt, result.WillRenew,
		result.Environment, rawPayload); err != nil {
		return fmt.Errorf("recording subscription transaction: %w", err)
	}
	return nil
}

func mergeIDs(a, b []int64) []int64 {
	seen := make(map[int64]struct{}, len(a)+len(b))
	out := make([]int64, 0, len(a)+len(b))
	for _, id := range append(a, b...) {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
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
