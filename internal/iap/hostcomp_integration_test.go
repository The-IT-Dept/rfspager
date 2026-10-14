package iap

import (
	"context"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

// withHostComp adds the receiver-host comp columns (migration 036) to the
// ownership fixture and gives user 1 a running comp.
func withHostComp(t *testing.T, db *sqlx.DB) time.Time {
	t.Helper()
	until := time.Now().UTC().Add(7 * 24 * time.Hour).Truncate(time.Second)
	if _, err := db.Exec(`ALTER TABLE users ADD COLUMN host_comp_until TIMESTAMPTZ, ADD COLUMN host_comp_since TIMESTAMPTZ`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE users SET host_comp_until = $1, host_comp_since = NOW() - INTERVAL '1 day' WHERE id = 1`, until); err != nil {
		t.Fatal(err)
	}
	return until
}

func hostCompOf(t *testing.T, db *sqlx.DB, userID int64) (until, since *time.Time) {
	t.Helper()
	if err := db.QueryRow(`SELECT host_comp_until, host_comp_since FROM users WHERE id = $1`, userID).Scan(&until, &since); err != nil {
		t.Fatal(err)
	}
	return until, since
}

// The keep-guard compares the store slot only. A running host comp is not
// in that slot, so a store purchase on a comped account is applied, and the
// comp columns are left exactly as they were.
func TestApplySubscriptionIgnoresHostComp(t *testing.T) {
	db := testOwnershipDB(t)
	until := withHostComp(t, db)
	_, sinceBefore := hostCompOf(t, db, 1)

	result := activeResult("apple", "pro.monthly", "Production")
	result.OriginalTransactionID = "apple-original-comped"
	if err := applySubscription(context.Background(), db, 1, result, "purchase", "receipt"); err != nil {
		t.Fatal(err)
	}
	var slot struct {
		Valid    bool    `db:"subscription_valid"`
		Product  *string `db:"subscription_product_id"`
		Platform *string `db:"subscription_platform"`
	}
	if err := db.Get(&slot, `SELECT subscription_valid, subscription_product_id, subscription_platform FROM users WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if !slot.Valid || slot.Product == nil || *slot.Product != "pro.monthly" || slot.Platform == nil || *slot.Platform != "apple" {
		t.Fatalf("store purchase on a comped account was not applied: %+v", slot)
	}
	gotUntil, gotSince := hostCompOf(t, db, 1)
	if gotUntil == nil || !gotUntil.Equal(until) || gotSince == nil || !gotSince.Equal(*sinceBefore) {
		t.Fatalf("store path touched the host comp: until %v since %v", gotUntil, gotSince)
	}

}

// The expiry sweep only looks at the store slot: a lapsed store
// subscription on a comped account is expired as usual, the comp is left
// alone, and a comp-only account is never touched or audited.
func TestExpirySweepIgnoresHostComp(t *testing.T) {
	db := testOwnershipDB(t) // users 1 and 2
	until := withHostComp(t, db)
	if _, err := db.Exec(`
		UPDATE users SET subscription_valid = true, subscription_platform = 'apple',
		       subscription_product_id = 'io.devlan.rfspager.monthly', subscription_expiry = NOW() - INTERVAL '90 days' WHERE id = 1;
		UPDATE users SET host_comp_until = NOW() + INTERVAL '3 days', host_comp_since = NOW() WHERE id = 2`); err != nil {
		t.Fatal(err)
	}
	ids, err := ExpireLapsedSubscriptions(context.Background(), db, ExpiryGrace)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("expired %v, want [1]", ids)
	}
	if gotUntil, gotSince := hostCompOf(t, db, 1); gotUntil == nil || !gotUntil.Equal(until) || gotSince == nil {
		t.Fatalf("sweep touched the host comp: until %v since %v", gotUntil, gotSince)
	}
	var audits int
	if err := db.Get(&audits, `SELECT count(*) FROM iap_transactions WHERE user_id = 2`); err != nil {
		t.Fatal(err)
	}
	if audits != 0 {
		t.Fatalf("comp-only account was audited by the sweep")
	}
}
