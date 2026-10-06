// One-off backfill: pull each user's subscription state out of Adapty's
// Server-Side API v2 and write it onto our users row so we can:
//
//  1. show correct subscription state for paid users who don't open the app
//     after the cutover; and
//  2. populate apple_original_transaction_id / google_purchase_token so
//     future Apple S2S + Google RTDN webhooks can find these users.
//
// Usage:
//
//	ADAPTY_SECRET_API_KEY=secret_live_... \
//	  go run ./cmd/adapty-backfill --dry-run --only-missing --limit 10
//
// Then drop --dry-run when you're happy.
//
// Safety notes:
//
//   - Adapty's customer_user_id is our users.user_id (UUID), same mapping
//     the Laravel SyncWithAdapty action used.
//   - We never DOWNGRADE a user based on Adapty data — if Adapty doesn't
//     know about a user, or has no access level, we leave the row alone.
//     The /iap/restore safety-net handles those when they next open the app.
//   - Each successful update writes an audit row to iap_transactions with
//     event_type='backfill:adapty'.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

const adaptyProfileURL = "https://api.adapty.io/api/v2/server-side-api/profile/"

var (
	dsn         = flag.String("dsn", envOr("DATABASE_URL", "postgres://127.0.0.1:5432/rfspager?sslmode=disable"), "Postgres DSN")
	apiKey      = flag.String("adapty-key", os.Getenv("ADAPTY_SECRET_API_KEY"), "Adapty secret API key (or set $ADAPTY_SECRET_API_KEY)")
	onlyMissing = flag.Bool("only-missing", true, "Only process users with no apple_original_transaction_id AND no google_purchase_token")
	onlyTxn     = flag.Bool("only-transacted", false, "Only process users who have ever transacted (adapty_id, subscription_expiry or subscription_valid set). Adapty has nothing to say about anyone else, so this skips the ~83% of rows that only cost API round-trips.")
	userIDFlag  = flag.String("user-id", "", "Only process this user (users.id integer OR users.user_id UUID)")
	limit       = flag.Int("limit", 0, "Stop after this many users (0 = no limit)")
	sleepMS     = flag.Int("sleep", 200, "Milliseconds to sleep between Adapty API calls (rate-limit guard)")
	dryRun      = flag.Bool("dry-run", false, "Print what would change, don't write")
	verbose     = flag.Bool("verbose", false, "Log every user attempted, not just changes")
)

func main() {
	flag.Parse()

	if *apiKey == "" {
		fail("missing --adapty-key (or set $ADAPTY_SECRET_API_KEY)")
	}

	conn := sqlx.MustConnect("postgres", *dsn)
	defer conn.Close()

	users, err := selectUsers(conn)
	if err != nil {
		fail("select users: %v", err)
	}
	fmt.Printf("backfilling %d user(s) from Adapty (dry-run=%v, only-missing=%v)\n", len(users), *dryRun, *onlyMissing)

	client := &http.Client{Timeout: 15 * time.Second}
	ctx := context.Background()

	var stats struct {
		updated, cleared, notFound, noAccess, errored int
	}

	for i, u := range users {
		if *limit > 0 && i >= *limit {
			break
		}
		if i > 0 {
			time.Sleep(time.Duration(*sleepMS) * time.Millisecond)
		}

		profile, status, err := fetchProfile(ctx, client, *apiKey, u.UserID)
		switch {
		case status == 404:
			stats.notFound++
			if *verbose {
				fmt.Printf("  %d %s — not in Adapty\n", u.ID, u.UserID)
			}
			continue
		case err != nil:
			stats.errored++
			fmt.Printf("  %d %s — ERROR %v\n", u.ID, u.UserID, err)
			continue
		}

		patch := derivePatch(profile)
		if patch == nil {
			stats.noAccess++
			if *verbose {
				fmt.Printf("  %d %s — no paid access in Adapty\n", u.ID, u.UserID)
			}
			continue
		}

		fmt.Printf("  %d %s — %s %s expires=%s renew=%v store_id=%s\n",
			u.ID, u.UserID, patch.Platform, patch.ProductID,
			fmtTime(patch.Expiry), patch.WillRenew, truncate(patch.StoreOriginalID, 16))

		if *dryRun {
			continue
		}

		if err := apply(ctx, conn, u.ID, patch); err != nil {
			stats.errored++
			fmt.Printf("    ! write failed: %v\n", err)
			continue
		}
		stats.updated++
	}

	fmt.Printf("\nresult: updated=%d cleared=%d not_in_adapty=%d no_access=%d errored=%d\n",
		stats.updated, stats.cleared, stats.notFound, stats.noAccess, stats.errored)
}

// ----------------------------------------------------------------------------
// DB
// ----------------------------------------------------------------------------

type userRow struct {
	ID     int64  `db:"id"`
	UserID string `db:"user_id"`
}

func selectUsers(conn *sqlx.DB) ([]userRow, error) {
	q := `SELECT id, user_id::text AS user_id FROM users WHERE user_id IS NOT NULL`
	args := []any{}

	if *userIDFlag != "" {
		// Allow either a numeric id or a UUID.
		if isAllDigits(*userIDFlag) {
			q += " AND id = $1"
		} else {
			q += " AND user_id = $1"
		}
		args = append(args, *userIDFlag)
	} else if *onlyMissing {
		q += ` AND (apple_original_transaction_id IS NULL OR apple_original_transaction_id = '')
		       AND (google_purchase_token IS NULL OR google_purchase_token = '')`
	}

	if *onlyTxn && *userIDFlag == "" {
		q += ` AND (adapty_id IS NOT NULL
		         OR subscription_expiry IS NOT NULL
		         OR subscription_valid = true)`
	}

	q += " ORDER BY id"

	var rows []userRow
	if err := conn.Select(&rows, q, args...); err != nil {
		return nil, err
	}
	return rows, nil
}

func apply(ctx context.Context, conn *sqlx.DB, userID int64, p *patch) error {
	tx, err := conn.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Update the user. Only set the platform-specific store ID column matching
	// the patch's platform; leave the other one alone.
	// $4 and $7 are each used both as an assigned value and inside a CASE
	// comparison, and they land on columns of differing types (varchar vs
	// text). Without explicit casts Postgres cannot deduce one type for the
	// parameter and rejects the statement with 42P08.
	//
	// Never shorten an entitlement granted elsewhere (a gift, a comp, a newer
	// purchase): when the account is valid past Adapty's expiry, keep its
	// subscription fields and only record the store IDs.
	q := `WITH cur AS (
		SELECT id, (subscription_valid AND (subscription_expiry IS NULL OR subscription_expiry > $2)) AS keep
		  FROM users WHERE id = $8 FOR UPDATE
	)
	UPDATE users u SET
		subscription_valid       = CASE WHEN cur.keep THEN u.subscription_valid ELSE $1 END,
		subscription_expiry      = CASE WHEN cur.keep THEN u.subscription_expiry ELSE $2 END,
		subscription_product_id  = CASE WHEN cur.keep THEN u.subscription_product_id ELSE $3 END,
		subscription_platform    = CASE WHEN cur.keep THEN u.subscription_platform ELSE $4::text END,
		will_renew               = CASE WHEN cur.keep THEN u.will_renew ELSE $5 END,
		cancellation_reason      = CASE WHEN cur.keep THEN u.cancellation_reason ELSE COALESCE(NULLIF($6::text, ''), u.cancellation_reason) END,
		apple_original_transaction_id = CASE WHEN $4::text = 'apple'  AND $7::text <> '' THEN $7::text ELSE u.apple_original_transaction_id END,
		google_purchase_token         = CASE WHEN $4::text = 'google' AND $7::text <> '' THEN $7::text ELSE u.google_purchase_token END,
		last_store_sync_at       = NOW(),
		updated_at               = NOW()
	  FROM cur
	 WHERE u.id = cur.id`
	if _, err := tx.ExecContext(ctx, q,
		p.Valid, p.Expiry, p.ProductID, p.Platform, p.WillRenew, p.CancellationReason, p.StoreOriginalID, userID,
	); err != nil {
		return fmt.Errorf("update users: %w", err)
	}

	// Record the shared entitlement link so store webhooks can fan out to this
	// account. Without it a backfilled row would be unreachable by matching
	// against the ownership ledger (migration 029). Only Apple/Google have a
	// ledger; a Stripe row is matched by users.stripe_id instead.
	if p.StoreOriginalID != "" && (p.Platform == "apple" || p.Platform == "google") {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO iap_subscription_ownership
				(platform, original_transaction_id, user_id, updated_at)
			VALUES ($1, $2, $3, NOW())
			ON CONFLICT (platform, original_transaction_id, user_id)
				DO UPDATE SET updated_at = NOW()`,
			p.Platform, p.StoreOriginalID, userID,
		); err != nil {
			return fmt.Errorf("link iap ownership: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO iap_transactions (
			user_id, platform, transaction_id, original_transaction_id, product_id,
			event_type, is_valid, expires_at, will_renew, environment, raw_payload
		) VALUES ($1, $2, $3, $4, $5, 'backfill:adapty', $6, $7, $8, $9, $10)`,
		userID, p.Platform, p.StoreTransactionID, p.StoreOriginalID, p.ProductID,
		p.Valid, p.Expiry, p.WillRenew, p.Environment, p.RawPayload,
	); err != nil {
		return fmt.Errorf("insert iap_transactions: %w", err)
	}

	return tx.Commit()
}

// ----------------------------------------------------------------------------
// Adapty API
// ----------------------------------------------------------------------------

type adaptyProfile struct {
	Data struct {
		ProfileID      string         `json:"profile_id"`
		CustomerUserID string         `json:"customer_user_id"`
		AccessLevels   []accessLevel  `json:"access_levels"`
		Subscriptions  []subscription `json:"subscriptions"`
	} `json:"data"`
}

type accessLevel struct {
	AccessLevelID          string  `json:"access_level_id"`
	Store                  string  `json:"store"`
	StoreProductID         string  `json:"store_product_id"`
	StoreTransactionID     string  `json:"store_transaction_id"`
	StoreOriginalTxnID     string  `json:"store_original_transaction_id"`
	Environment            string  `json:"environment"`
	StartsAt               *string `json:"starts_at"`
	PurchasedAt            *string `json:"purchased_at"`
	ExpiresAt              *string `json:"expires_at"`
	RenewalCancelledAt     *string `json:"renewal_cancelled_at"`
	BillingIssueDetectedAt *string `json:"billing_issue_detected_at"`
	IsInGracePeriod        *bool   `json:"is_in_grace_period"`
	CancellationReason     string  `json:"cancellation_reason"`
}

type subscription struct {
	Store              string  `json:"store"`
	StoreProductID     string  `json:"store_product_id"`
	StoreTransactionID string  `json:"store_transaction_id"`
	StoreOriginalTxnID string  `json:"store_original_transaction_id"`
	Environment        string  `json:"environment"`
	PurchasedAt        *string `json:"purchased_at"`
	RefundedAt         *string `json:"refunded_at"`
	ExpiresAt          *string `json:"expires_at"`
	RenewStatus        *int    `json:"renew_status"`
	CancellationReason string  `json:"cancellation_reason"`
}

func fetchProfile(ctx context.Context, client *http.Client, key, customerUserID string) (*adaptyProfile, int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", adaptyProfileURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Api-Key "+key)
	req.Header.Set("adapty-customer-user-id", customerUserID)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == 429 {
		// Caller will surface this in stats; sleep a bit so we don't hammer.
		time.Sleep(2 * time.Second)
		return nil, resp.StatusCode, fmt.Errorf("rate-limited (429)")
	}
	if resp.StatusCode == 404 {
		return nil, 404, nil
	}
	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, fmt.Errorf("adapty %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var p adaptyProfile
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode: %w (body=%s)", err, truncate(string(body), 200))
	}
	// Attach the raw payload to the parsed profile via a side channel so the
	// audit log row can record what we saw. Simpler than threading bytes everywhere.
	rawPayloadByProfile[p.Data.ProfileID] = string(body)
	return &p, resp.StatusCode, nil
}

// Side-channel for raw payloads keyed by Adapty profile_id. We use this from
// derivePatch() so the iap_transactions audit row gets the bytes.
var rawPayloadByProfile = map[string]string{}

// ----------------------------------------------------------------------------
// Derivation: Adapty profile → our users-row patch
// ----------------------------------------------------------------------------

type patch struct {
	Platform           string // 'apple' / 'google' / 'stripe'
	ProductID          string
	Expiry             time.Time
	Valid              bool
	WillRenew          bool
	CancellationReason string
	StoreOriginalID    string // → apple_original_transaction_id or google_purchase_token
	StoreTransactionID string // for iap_transactions row
	Environment        string
	RawPayload         string
}

// derivePatch picks the most useful access-level/subscription from the
// profile. Returns nil if Adapty knows about this user but doesn't think
// they have any paid access. We never DOWNGRADE here — that case is left
// to the in-app /iap/restore safety net.
func derivePatch(p *adaptyProfile) *patch {
	// 1. Prefer a "premium" access level. That's the one Laravel was
	//    looking up, and it's the canonical "is this user paid" signal.
	var al *accessLevel
	for i := range p.Data.AccessLevels {
		if p.Data.AccessLevels[i].AccessLevelID == "premium" {
			al = &p.Data.AccessLevels[i]
			break
		}
	}
	// Fall back to the first access level if there's no explicit "premium"
	// — some old profiles used a different key.
	if al == nil && len(p.Data.AccessLevels) > 0 {
		al = &p.Data.AccessLevels[0]
	}

	// 2. Find the matching subscription by product_id — it carries the
	//    will-renew / cancellation-reason / refunded_at signals the access
	//    level doesn't.
	var sub *subscription
	if al != nil {
		for i := range p.Data.Subscriptions {
			if p.Data.Subscriptions[i].StoreProductID == al.StoreProductID {
				sub = &p.Data.Subscriptions[i]
				break
			}
		}
	}
	// If there's no access level but there's an active subscription, use that.
	if al == nil && len(p.Data.Subscriptions) > 0 {
		for i := range p.Data.Subscriptions {
			s := p.Data.Subscriptions[i]
			if parseExpiry(s.ExpiresAt).After(time.Now()) && s.RefundedAt == nil {
				sub = &s
				break
			}
		}
	}

	if al == nil && sub == nil {
		return nil
	}

	// Decide canonical source of truth: prefer access level fields, fall
	// back to subscription where access level is silent.
	platform := normalisePlatform(coalesce(getStore(al), getStore(sub)))
	if platform == "" {
		// No app_store / play_store info → we can't help.
		return nil
	}

	expiry := parseExpiry(coalescePtr(getExpiry(al), getExpiry(sub)))
	productID := coalesce(getString(al, "ProductID"), getString(sub, "ProductID"))
	storeOriginal := coalesce(getString(al, "Original"), getString(sub, "Original"))
	// For Google, Adapty's profile API only exposes the Play *order* ID
	// (GPA.…), not the purchase token RTDN and the Play API key on. Storing it
	// as google_purchase_token would never match; the Adapty webhook records
	// the real token on the user's next event instead.
	if platform == "google" && strings.HasPrefix(storeOriginal, "GPA.") {
		storeOriginal = ""
	}
	storeTxn := coalesce(getString(al, "Txn"), getString(sub, "Txn"))
	env := coalesce(getString(al, "Env"), getString(sub, "Env"))
	cancelReason := coalesce(getString(al, "Cancel"), getString(sub, "Cancel"))

	valid := expiry.After(time.Now())
	if sub != nil && sub.RefundedAt != nil {
		valid = false
		if cancelReason == "" {
			cancelReason = "refund"
		}
	}
	if al != nil && al.IsInGracePeriod != nil && *al.IsInGracePeriod {
		// In grace — keep them valid.
		valid = true
	}

	willRenew := true
	if sub != nil && sub.RenewStatus != nil {
		willRenew = *sub.RenewStatus == 1
	}
	if al != nil && al.RenewalCancelledAt != nil && *al.RenewalCancelledAt != "" {
		willRenew = false
	}

	return &patch{
		Platform:           platform,
		ProductID:          productID,
		Expiry:             expiry,
		Valid:              valid,
		WillRenew:          willRenew,
		CancellationReason: cancelReason,
		StoreOriginalID:    storeOriginal,
		StoreTransactionID: storeTxn,
		Environment:        env,
		RawPayload:         rawPayloadByProfile[p.Data.ProfileID],
	}
}

// ----------------------------------------------------------------------------
// Tiny helpers — keep derivePatch readable.
// ----------------------------------------------------------------------------

// normalisePlatform maps Adapty's `store` values to ours.
func normalisePlatform(s string) string {
	switch strings.ToLower(s) {
	case "app_store", "apple":
		return "apple"
	case "play_store", "google", "google_play":
		return "google"
	case "stripe", "web":
		return "stripe"
	default:
		return ""
	}
}

// getStore / getExpiry / getString are tiny field-pickers that accept
// either an *accessLevel or a *subscription so derivePatch can fall back
// from one to the other without a wall of nil-checks. Each returns the
// zero value when the argument is nil.

func getStore(v any) string {
	switch x := v.(type) {
	case *accessLevel:
		if x == nil {
			return ""
		}
		return x.Store
	case *subscription:
		if x == nil {
			return ""
		}
		return x.Store
	}
	return ""
}

func getExpiry(v any) *string {
	switch x := v.(type) {
	case *accessLevel:
		if x == nil {
			return nil
		}
		return x.ExpiresAt
	case *subscription:
		if x == nil {
			return nil
		}
		return x.ExpiresAt
	}
	return nil
}

// getString pulls a named field. The keys are short codes to keep
// derivePatch readable: ProductID, Original, Txn, Env, Cancel.
func getString(v any, key string) string {
	switch x := v.(type) {
	case *accessLevel:
		if x == nil {
			return ""
		}
		switch key {
		case "ProductID":
			return x.StoreProductID
		case "Original":
			return x.StoreOriginalTxnID
		case "Txn":
			return x.StoreTransactionID
		case "Env":
			return x.Environment
		case "Cancel":
			return x.CancellationReason
		}
	case *subscription:
		if x == nil {
			return ""
		}
		switch key {
		case "ProductID":
			return x.StoreProductID
		case "Original":
			return x.StoreOriginalTxnID
		case "Txn":
			return x.StoreTransactionID
		case "Env":
			return x.Environment
		case "Cancel":
			return x.CancellationReason
		}
	}
	return ""
}

// ----------------------------------------------------------------------------
// Generic helpers
// ----------------------------------------------------------------------------

func parseExpiry(s *string) time.Time {
	if s == nil || *s == "" {
		return time.Time{}
	}
	// Adapty timestamps are RFC3339 (`2025-01-05T13:27:47.461425+00:00`).
	t, err := time.Parse(time.RFC3339Nano, *s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, *s)
	}
	return t
}

func coalesce(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func coalescePtr(vals ...*string) *string {
	for _, v := range vals {
		if v != nil && *v != "" {
			return v
		}
	}
	return nil
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format("2006-01-02")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "adapty-backfill: "+format+"\n", args...)
	os.Exit(1)
}
