package iap

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func testOwnershipDB(t *testing.T) *sqlx.DB {
	t.Helper()
	rawURL := os.Getenv("RFSPAGER_TEST_DATABASE_URL")
	if rawURL == "" {
		t.Skip("RFSPAGER_TEST_DATABASE_URL is not set")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse RFSPAGER_TEST_DATABASE_URL: %v", err)
	}
	if !strings.HasPrefix(strings.TrimPrefix(u.Path, "/"), "rfspager_test") {
		t.Fatalf("refusing to reset non-test database %q", u.Path)
	}
	db, err := sqlx.Connect("postgres", rawURL)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
		DROP SCHEMA public CASCADE;
		CREATE SCHEMA public;
		CREATE TABLE users (
			id BIGSERIAL PRIMARY KEY,
			subscription_valid BOOLEAN NOT NULL DEFAULT false,
			subscription_expiry TIMESTAMPTZ,
			subscription_product_id TEXT,
			subscription_platform TEXT,
			will_renew BOOLEAN,
			cancellation_reason TEXT,
			apple_original_transaction_id TEXT,
			google_purchase_token TEXT,
			last_store_sync_at TIMESTAMPTZ,
			updated_at TIMESTAMPTZ
		);
		CREATE TABLE iap_transactions (
			id BIGSERIAL PRIMARY KEY, user_id BIGINT NOT NULL REFERENCES users(id),
			platform TEXT NOT NULL, transaction_id TEXT, original_transaction_id TEXT,
			product_id TEXT NOT NULL, event_type TEXT NOT NULL, is_valid BOOLEAN NOT NULL,
			expires_at TIMESTAMPTZ, will_renew BOOLEAN, environment TEXT, raw_payload TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE TABLE iap_subscription_ownership (
			platform TEXT NOT NULL,
			original_transaction_id TEXT NOT NULL,
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (platform, original_transaction_id, user_id)
		);
		INSERT INTO users DEFAULT VALUES;
		INSERT INTO users DEFAULT VALUES;
		CREATE INDEX users_apple_original_transaction_id_idx
			ON users (apple_original_transaction_id) WHERE apple_original_transaction_id IS NOT NULL;
		CREATE INDEX users_google_purchase_token_idx
			ON users (google_purchase_token) WHERE google_purchase_token IS NOT NULL;
	`); err != nil {
		t.Fatalf("create ownership fixture: %v", err)
	}
	return db
}

func TestApplySubscriptionKeepsAppleOwnershipWithFirstUser(t *testing.T) {
	db := testOwnershipDB(t)
	result := activeResult("apple", "pro.monthly", "Production")
	result.OriginalTransactionID = "apple-original-123"

	if err := applySubscription(context.Background(), db, 1, result, "purchase", "receipt"); err != nil {
		t.Fatalf("first user claim: %v", err)
	}
	if err := applySubscription(context.Background(), db, 1, result, "restore", "receipt"); err != nil {
		t.Fatalf("same user restore: %v", err)
	}
	var logCount int
	if err := db.Get(&logCount, `SELECT COUNT(*) FROM iap_transactions WHERE user_id = 1`); err != nil {
		t.Fatalf("count transaction log: %v", err)
	}
	if logCount != 2 {
		t.Fatalf("transaction log rows = %d, want 2", logCount)
	}
}

func TestApplySubscriptionAllowsTwoAccountsToShareAppleEntitlement(t *testing.T) {
	db := testOwnershipDB(t)
	result := activeResult("apple", "pro.monthly", "Production")
	result.OriginalTransactionID = "apple-original-456"
	if err := applySubscription(context.Background(), db, 1, result, "purchase", "receipt"); err != nil {
		t.Fatalf("first user claim: %v", err)
	}
	if err := applySubscription(context.Background(), db, 2, result, "restore", "receipt"); err != nil {
		t.Fatalf("second user restore: %v", err)
	}

	var linked int
	if err := db.Get(&linked, `SELECT COUNT(*) FROM iap_subscription_ownership WHERE original_transaction_id = $1`, result.OriginalTransactionID); err != nil {
		t.Fatalf("count linked accounts: %v", err)
	}
	if linked != 2 {
		t.Fatalf("linked accounts = %d, want 2", linked)
	}
	var secondUserActive bool
	if err := db.Get(&secondUserActive, `SELECT subscription_valid FROM users WHERE id = 2`); err != nil {
		t.Fatalf("read second user: %v", err)
	}
	if !secondUserActive {
		t.Fatal("shared purchase did not grant the second user access")
	}
}

func TestApplySubscriptionAllowsNewApplePurchaseForSameUserAndRetainsOldClaim(t *testing.T) {
	db := testOwnershipDB(t)
	first := activeResult("apple", "pro.monthly", "Production")
	first.OriginalTransactionID = "apple-first-original"
	if err := applySubscription(context.Background(), db, 1, first, "purchase", "receipt"); err != nil {
		t.Fatalf("first Apple purchase: %v", err)
	}
	second := activeResult("apple", "pro.monthly", "Production")
	second.OriginalTransactionID = "apple-second-original"
	if err := applySubscription(context.Background(), db, 1, second, "purchase", "receipt"); err != nil {
		t.Fatalf("new Apple purchase for same user: %v", err)
	}

	// The old receipt stays linked and a second account can now share it too.
	if err := applySubscription(context.Background(), db, 2, first, "restore", "receipt"); err != nil {
		t.Fatalf("old Apple purchase shared after replacement: %v", err)
	}
	var linked int
	if err := db.Get(&linked, `SELECT COUNT(*) FROM iap_subscription_ownership WHERE original_transaction_id = $1`, first.OriginalTransactionID); err != nil {
		t.Fatalf("count old-receipt links: %v", err)
	}
	if linked != 2 {
		t.Fatalf("old-receipt links = %d, want 2", linked)
	}
}

func TestApplySubscriptionAllowsTwoAccountsToShareGoogleEntitlement(t *testing.T) {
	db := testOwnershipDB(t)
	result := activeResult("google", "pro.monthly", "Production")
	result.OriginalTransactionID = "google-token-789"
	if err := applySubscription(context.Background(), db, 1, result, "purchase", "receipt"); err != nil {
		t.Fatalf("first user claim: %v", err)
	}
	if err := applySubscription(context.Background(), db, 2, result, "restore", "receipt"); err != nil {
		t.Fatalf("second user restore: %v", err)
	}
	var linked int
	if err := db.Get(&linked, `SELECT COUNT(*) FROM iap_subscription_ownership WHERE original_transaction_id = $1`, result.OriginalTransactionID); err != nil {
		t.Fatalf("count linked accounts: %v", err)
	}
	if linked != 2 {
		t.Fatalf("linked accounts = %d, want 2", linked)
	}
}

func TestApplySubscriptionAllowsGoogleUpgradeOnAnotherAccount(t *testing.T) {
	db := testOwnershipDB(t)
	old := activeResult("google", "pro.monthly", "Production")
	old.OriginalTransactionID = "google-old-token"
	if err := applySubscription(context.Background(), db, 1, old, "purchase", "receipt"); err != nil {
		t.Fatalf("first user old-token claim: %v", err)
	}

	upgraded := activeResult("google", "pro.yearly", "Production")
	upgraded.OriginalTransactionID = "google-upgraded-token"
	upgraded.PreviousOwnershipID = old.OriginalTransactionID
	if err := applySubscription(context.Background(), db, 2, upgraded, "purchase", "receipt"); err != nil {
		t.Fatalf("upgrade on another account: %v", err)
	}
	var bound string
	if err := db.Get(&bound, `SELECT google_purchase_token FROM users WHERE id = 2`); err != nil {
		t.Fatalf("read upgraded binding: %v", err)
	}
	if bound != upgraded.OriginalTransactionID {
		t.Fatalf("bound token = %q, want new Google token", bound)
	}
}

func TestApplySubscriptionAllowsGoogleUpgradeLinkedToSameUser(t *testing.T) {
	db := testOwnershipDB(t)
	old := activeResult("google", "pro.monthly", "Production")
	old.OriginalTransactionID = "google-same-user-old-token"
	if err := applySubscription(context.Background(), db, 1, old, "purchase", "receipt"); err != nil {
		t.Fatalf("first user old-token claim: %v", err)
	}

	upgraded := activeResult("google", "pro.yearly", "Production")
	upgraded.OriginalTransactionID = "google-same-user-new-token"
	upgraded.PreviousOwnershipID = old.OriginalTransactionID
	if err := applySubscription(context.Background(), db, 1, upgraded, "purchase", "receipt"); err != nil {
		t.Fatalf("same user upgrade: %v", err)
	}

	var boundToken string
	if err := db.Get(&boundToken, `SELECT google_purchase_token FROM users WHERE id = 1`); err != nil {
		t.Fatalf("read upgraded binding: %v", err)
	}
	if boundToken != upgraded.OriginalTransactionID {
		t.Fatalf("bound token = %q, want new Google token", boundToken)
	}
}

func TestApplySubscriptionRejectsGoogleUpgradeLinkedToHistoricalOwner(t *testing.T) {
	db := testOwnershipDB(t)
	old := activeResult("google", "pro.monthly", "Production")
	old.OriginalTransactionID = "google-historical-old-token"
	if err := applySubscription(context.Background(), db, 1, old, "purchase", "receipt"); err != nil {
		t.Fatalf("first user old-token claim: %v", err)
	}
	new := activeResult("google", "pro.yearly", "Production")
	new.OriginalTransactionID = "google-historical-new-token"
	new.PreviousOwnershipID = old.OriginalTransactionID
	if err := applySubscription(context.Background(), db, 1, new, "purchase", "receipt"); err != nil {
		t.Fatalf("first user upgrade: %v", err)
	}

	replay := activeResult("google", "pro.yearly", "Production")
	replay.OriginalTransactionID = "google-attacker-new-token"
	replay.PreviousOwnershipID = old.OriginalTransactionID
	if err := applySubscription(context.Background(), db, 2, replay, "purchase", "receipt"); err != nil {
		t.Fatalf("historical linked token replayed by another user: %v", err)
	}
	var bound string
	if err := db.Get(&bound, `SELECT google_purchase_token FROM users WHERE id = 2`); err != nil {
		t.Fatalf("read replay binding: %v", err)
	}
	if bound != replay.OriginalTransactionID {
		t.Fatalf("replay bound token = %q, want %q", bound, replay.OriginalTransactionID)
	}
}

func TestApplySubscriptionConcurrentAppleClaimsBothLink(t *testing.T) {
	db := testOwnershipDB(t)
	result := activeResult("apple", "pro.monthly", "Production")
	result.OriginalTransactionID = "apple-concurrent-claim"

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, userID := range []int64{1, 2} {
		wg.Add(1)
		go func(userID int64) {
			defer wg.Done()
			<-start
			errs <- applySubscription(context.Background(), db, userID, result, "purchase", "receipt")
		}(userID)
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent claim error = %v", err)
		}
	}
	var linked int
	if err := db.Get(&linked, `SELECT COUNT(*) FROM iap_subscription_ownership WHERE original_transaction_id = $1`, result.OriginalTransactionID); err != nil {
		t.Fatalf("count concurrent links: %v", err)
	}
	if linked != 2 {
		t.Fatalf("concurrent links = %d, want 2", linked)
	}
}

func TestApplyWebhookSubscriptionFanOutUpdatesEverySharers(t *testing.T) {
	db := testOwnershipDB(t)
	result := activeResult("apple", "pro.monthly", "Production")
	result.OriginalTransactionID = "apple-shared-fanout"
	for _, userID := range []int64{1, 2} {
		if err := applySubscription(context.Background(), db, userID, result, "purchase", "receipt"); err != nil {
			t.Fatalf("link user %d: %v", userID, err)
		}
	}

	result.Valid = false
	updated, err := applyWebhookSubscription(context.Background(), db, result, "webhook:EXPIRED", "receipt", "")
	if err != nil {
		t.Fatalf("apply shared Apple webhook: %v", err)
	}
	if !updated {
		t.Fatal("shared webhook reported no change")
	}
	for _, userID := range []int64{1, 2} {
		var active bool
		if err := db.Get(&active, `SELECT subscription_valid FROM users WHERE id = $1`, userID); err != nil {
			t.Fatalf("read user %d: %v", userID, err)
		}
		if active {
			t.Fatalf("user %d still active after expiry fan-out", userID)
		}
	}
}

func TestApplyWebhookSubscriptionLogsHistoricalAppleEventWithoutDowngradingCurrentPurchase(t *testing.T) {
	db := testOwnershipDB(t)
	old := activeResult("apple", "pro.monthly", "Production")
	old.OriginalTransactionID = "apple-webhook-old"
	if err := applySubscription(context.Background(), db, 1, old, "purchase", "receipt"); err != nil {
		t.Fatalf("old Apple purchase: %v", err)
	}
	current := activeResult("apple", "pro.monthly", "Production")
	current.OriginalTransactionID = "apple-webhook-current"
	if err := applySubscription(context.Background(), db, 1, current, "purchase", "receipt"); err != nil {
		t.Fatalf("current Apple purchase: %v", err)
	}
	old.Valid = false
	updated, err := applyWebhookSubscription(context.Background(), db, old, "webhook:EXPIRED", "receipt", "")
	if err != nil {
		t.Fatalf("apply old Apple webhook: %v", err)
	}
	if updated {
		t.Fatal("historical Apple webhook updated the current subscription")
	}
	var active, bound string
	if err := db.QueryRow(`SELECT subscription_valid::text, apple_original_transaction_id FROM users WHERE id = 1`).Scan(&active, &bound); err != nil {
		t.Fatalf("read current Apple subscription: %v", err)
	}
	if active != "true" || bound != current.OriginalTransactionID {
		t.Fatalf("historical webhook left active=%q bound=%q", active, bound)
	}
}

func TestApplyWebhookSubscriptionDoesNotDowngradeNewerGoogleToken(t *testing.T) {
	db := testOwnershipDB(t)
	old := activeResult("google", "pro.monthly", "Production")
	old.OriginalTransactionID = "google-webhook-old"
	if err := applySubscription(context.Background(), db, 1, old, "purchase", "receipt"); err != nil {
		t.Fatalf("old Google purchase: %v", err)
	}
	current := activeResult("google", "pro.yearly", "Production")
	current.OriginalTransactionID = "google-webhook-current"
	current.PreviousOwnershipID = old.OriginalTransactionID
	if err := applySubscription(context.Background(), db, 1, current, "purchase", "receipt"); err != nil {
		t.Fatalf("current Google purchase: %v", err)
	}
	old.Valid = false
	updated, err := applyWebhookSubscription(context.Background(), db, old, "webhook", "receipt", "")
	if err != nil {
		t.Fatalf("apply old Google webhook: %v", err)
	}
	if updated {
		t.Fatal("historical Google webhook updated the current subscription")
	}
	var active, bound string
	if err := db.QueryRow(`SELECT subscription_valid::text, google_purchase_token FROM users WHERE id = 1`).Scan(&active, &bound); err != nil {
		t.Fatalf("read current Google subscription: %v", err)
	}
	if active != "true" || bound != current.OriginalTransactionID {
		t.Fatalf("historical webhook left active=%q bound=%q", active, bound)
	}
}

func TestDelayedAppleWebhookCannotDowngradeCurrentGoogleSubscription(t *testing.T) {
	db := testOwnershipDB(t)
	apple := activeResult("apple", "pro.monthly", "Production")
	apple.OriginalTransactionID = "apple-before-google"
	if err := applySubscription(context.Background(), db, 1, apple, "purchase", "receipt"); err != nil {
		t.Fatalf("Apple purchase: %v", err)
	}
	google := activeResult("google", "pro.monthly", "Production")
	google.OriginalTransactionID = "google-current"
	if err := applySubscription(context.Background(), db, 1, google, "purchase", "receipt"); err != nil {
		t.Fatalf("Google purchase: %v", err)
	}
	apple.Valid = false
	updated, err := applyWebhookSubscription(context.Background(), db, apple, "webhook:EXPIRED", "receipt", "")
	if err != nil {
		t.Fatalf("apply delayed Apple webhook: %v", err)
	}
	if updated {
		t.Fatal("delayed Apple webhook updated the current Google subscription")
	}
	var active bool
	var platform string
	if err := db.QueryRow(`SELECT subscription_valid, subscription_platform FROM users WHERE id = 1`).Scan(&active, &platform); err != nil {
		t.Fatalf("read current subscription: %v", err)
	}
	if !active || platform != "google" {
		t.Fatalf("delayed Apple webhook left active=%t platform=%q", active, platform)
	}
}

func TestDelayedGoogleWebhookCannotDowngradeCurrentAppleSubscription(t *testing.T) {
	db := testOwnershipDB(t)
	google := activeResult("google", "pro.monthly", "Production")
	google.OriginalTransactionID = "google-before-apple"
	if err := applySubscription(context.Background(), db, 1, google, "purchase", "receipt"); err != nil {
		t.Fatalf("Google purchase: %v", err)
	}
	apple := activeResult("apple", "pro.monthly", "Production")
	apple.OriginalTransactionID = "apple-current"
	if err := applySubscription(context.Background(), db, 1, apple, "purchase", "receipt"); err != nil {
		t.Fatalf("Apple purchase: %v", err)
	}
	google.Valid = false
	updated, err := applyWebhookSubscription(context.Background(), db, google, "webhook", "receipt", "")
	if err != nil {
		t.Fatalf("apply delayed Google webhook: %v", err)
	}
	if updated {
		t.Fatal("delayed Google webhook updated the current Apple subscription")
	}
	var active bool
	var platform string
	if err := db.QueryRow(`SELECT subscription_valid, subscription_platform FROM users WHERE id = 1`).Scan(&active, &platform); err != nil {
		t.Fatalf("read current subscription: %v", err)
	}
	if !active || platform != "apple" {
		t.Fatalf("delayed Google webhook left active=%t platform=%q", active, platform)
	}
}

// seedEntitlement gives a user an entitlement that was not granted by the
// store the app is about to validate against: a gift, a comp, or a purchase on
// the other platform.
func seedEntitlement(t *testing.T, db *sqlx.DB, userID int64, valid bool, expiresIn time.Duration, productID string) {
	t.Helper()
	if _, err := db.Exec(`UPDATE users SET subscription_valid = $2, subscription_expiry = NOW() + $3 * interval '1 second',
		subscription_product_id = $4, subscription_platform = NULL, will_renew = false WHERE id = $1`,
		userID, valid, expiresIn.Seconds(), productID); err != nil {
		t.Fatalf("seed entitlement: %v", err)
	}
}

type entitlementRow struct {
	Valid     bool           `db:"subscription_valid"`
	Expiry    time.Time      `db:"subscription_expiry"`
	ProductID sql.NullString `db:"subscription_product_id"`
	Platform  sql.NullString `db:"subscription_platform"`
	Token     sql.NullString `db:"google_purchase_token"`
}

func readEntitlement(t *testing.T, db *sqlx.DB, userID int64) entitlementRow {
	t.Helper()
	var row entitlementRow
	if err := db.Get(&row, `SELECT subscription_valid, subscription_expiry, subscription_product_id,
		subscription_platform, google_purchase_token FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("read entitlement: %v", err)
	}
	return row
}

// A store validation only speaks for its own subscription. When the account
// already holds access running past the store expiry (a gift), the app
// re-validating its Play purchase must not shorten that access or relabel it.
func TestApplySubscriptionKeepsLongerExistingEntitlement(t *testing.T) {
	db := testOwnershipDB(t)
	seedEntitlement(t, db, 1, true, 60*24*time.Hour, "io.devlan.rfspager.gifted")

	result := activeResult("google", "pro.monthly", "Production")
	result.OriginalTransactionID = "google-token-behind-gift"
	if err := applySubscription(context.Background(), db, 1, result, "purchase", "receipt"); err != nil {
		t.Fatalf("validate store purchase: %v", err)
	}

	row := readEntitlement(t, db, 1)
	if !row.Valid {
		t.Fatal("gift was switched off")
	}
	if row.ProductID.String != "io.devlan.rfspager.gifted" {
		t.Fatalf("product = %q, want the gift kept", row.ProductID.String)
	}
	if row.Platform.Valid {
		t.Fatalf("platform = %q, want NULL kept for the gift", row.Platform.String)
	}
	if row.Expiry.Before(time.Now().Add(59 * 24 * time.Hour)) {
		t.Fatalf("expiry = %s, want the gift's 60 days kept", row.Expiry)
	}
	// The store identity is still recorded so webhooks can find the account.
	if row.Token.String != result.OriginalTransactionID {
		t.Fatalf("google token = %q, want %q recorded", row.Token.String, result.OriginalTransactionID)
	}
	var linked, logged int
	if err := db.Get(&linked, `SELECT COUNT(*) FROM iap_subscription_ownership WHERE original_transaction_id = $1 AND user_id = 1`, result.OriginalTransactionID); err != nil {
		t.Fatalf("count ownership: %v", err)
	}
	if linked != 1 {
		t.Fatalf("ownership links = %d, want 1", linked)
	}
	if err := db.Get(&logged, `SELECT COUNT(*) FROM iap_transactions WHERE user_id = 1`); err != nil {
		t.Fatalf("count transaction log: %v", err)
	}
	if logged != 1 {
		t.Fatalf("transaction log rows = %d, want 1", logged)
	}
}

// The guard only protects access that outlives the store subscription: a
// shorter or lapsed entitlement is replaced as before.
func TestApplySubscriptionReplacesShorterExistingEntitlement(t *testing.T) {
	db := testOwnershipDB(t)
	seedEntitlement(t, db, 1, true, 10*time.Minute, "io.devlan.rfspager.gifted")

	result := activeResult("google", "pro.monthly", "Production")
	result.OriginalTransactionID = "google-token-after-short-gift"
	if err := applySubscription(context.Background(), db, 1, result, "purchase", "receipt"); err != nil {
		t.Fatalf("validate store purchase: %v", err)
	}

	row := readEntitlement(t, db, 1)
	if row.ProductID.String != "pro.monthly" || row.Platform.String != "google" {
		t.Fatalf("product/platform = %q/%q, want the store purchase applied", row.ProductID.String, row.Platform.String)
	}
	if row.Expiry.Before(time.Now().Add(50 * time.Minute)) {
		t.Fatalf("expiry = %s, want the store expiry (about an hour out)", row.Expiry)
	}
}

func TestApplySubscriptionReplacesLapsedEntitlementWithLaterExpiry(t *testing.T) {
	db := testOwnershipDB(t)
	// Switched off by an admin or the sweep, but the stale expiry is still in the future.
	seedEntitlement(t, db, 1, false, 60*24*time.Hour, "io.devlan.rfspager.gifted")

	result := activeResult("google", "pro.monthly", "Production")
	result.OriginalTransactionID = "google-token-after-lapsed-gift"
	if err := applySubscription(context.Background(), db, 1, result, "purchase", "receipt"); err != nil {
		t.Fatalf("validate store purchase: %v", err)
	}

	row := readEntitlement(t, db, 1)
	if !row.Valid || row.ProductID.String != "pro.monthly" {
		t.Fatalf("valid/product = %v/%q, want the store purchase to grant access", row.Valid, row.ProductID.String)
	}
}
