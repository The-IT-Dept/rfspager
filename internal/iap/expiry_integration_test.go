package iap

import (
	"context"
	"testing"
	"time"
)

func TestExpireLapsedSubscriptions(t *testing.T) {
	db := testOwnershipDB(t) // users 1 and 2 exist
	if _, err := db.Exec(`
		INSERT INTO users DEFAULT VALUES; INSERT INTO users DEFAULT VALUES;
		INSERT INTO users DEFAULT VALUES; INSERT INTO users DEFAULT VALUES;
		INSERT INTO users DEFAULT VALUES; INSERT INTO users DEFAULT VALUES;
		UPDATE users SET subscription_valid = true, subscription_platform = 'apple',
		       subscription_product_id = 'io.devlan.rfspager.monthly';
		-- 1: lapsed 90 days ago, no store event: expire
		UPDATE users SET subscription_expiry = NOW() - interval '90 days' WHERE id = 1;
		-- 2: 10 days past expiry, maybe in store billing grace: keep
		UPDATE users SET subscription_expiry = NOW() - interval '10 days' WHERE id = 2;
		-- 3: active: keep
		UPDATE users SET subscription_expiry = NOW() + interval '20 days' WHERE id = 3;
		-- 4: gifted, long expired: never touched
		UPDATE users SET subscription_expiry = NOW() - interval '400 days', subscription_product_id = 'io.devlan.rfspager.gifted' WHERE id = 4;
		-- 5: comp with no expiry: never touched
		UPDATE users SET subscription_expiry = NULL WHERE id = 5;
		-- 6: already invalid: not re-recorded
		UPDATE users SET subscription_valid = false, subscription_expiry = NOW() - interval '90 days' WHERE id = 6;
		-- 7: gift from "Give free time", a day past expiry: expire, no grace
		UPDATE users SET subscription_expiry = NOW() - interval '1 day', subscription_product_id = 'io.devlan.rfspager.gifted', subscription_platform = NULL WHERE id = 7;
		-- 8: gift from "Give free time", still running: keep
		UPDATE users SET subscription_expiry = NOW() + interval '1 day', subscription_product_id = 'io.devlan.rfspager.gifted', subscription_platform = NULL WHERE id = 8;
		INSERT INTO account_extensions (user_id, method, status) VALUES (7, 'gift', 'applied'), (8, 'gift', 'applied'), (4, 'gift', 'failed');
	`); err != nil {
		t.Fatal(err)
	}

	ids, err := ExpireLapsedSubscriptions(context.Background(), db, 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0]+ids[1] != 8 {
		t.Fatalf("expired %v, want [1 7]", ids)
	}
	var valid []bool
	if err := db.Select(&valid, `SELECT subscription_valid FROM users ORDER BY id`); err != nil {
		t.Fatal(err)
	}
	want := []bool{false, true, true, true, true, false, false, true}
	for i := range want {
		if valid[i] != want[i] {
			t.Fatalf("user %d valid=%v want %v", i+1, valid[i], want[i])
		}
	}
	var audit int
	_ = db.Get(&audit, `SELECT count(*) FROM iap_transactions WHERE user_id = 1 AND event_type = 'expiry:sweep' AND NOT is_valid`)
	if audit != 1 {
		t.Fatalf("audit rows = %d", audit)
	}
	var giftAudit string
	_ = db.Get(&giftAudit, `SELECT platform FROM iap_transactions WHERE user_id = 7 AND event_type = 'expiry:sweep'`)
	if giftAudit != "gift" {
		t.Fatalf("gift lapse audit platform = %q", giftAudit)
	}
	// A second run finds nothing more to do.
	if again, _ := ExpireLapsedSubscriptions(context.Background(), db, 30*24*time.Hour); len(again) != 0 {
		t.Fatalf("second run expired %v", again)
	}
}
