package dispatch

import (
	"context"
	"testing"
)

// Push fan-out goes to everyone with Pro: a valid store subscription or a
// running host comp. A lapsed comp or a free account gets nothing, through
// both a capcode subscription and an alert zone.
func TestFanoutIncludesHostCompedUsers(t *testing.T) {
	conn := fanoutTestDB(t)
	ctx := context.Background()

	user := func(email, set string) int64 {
		id := mustID(t, conn, `INSERT INTO users (user_id, email, password) VALUES (gen_random_uuid(), $1, '!') RETURNING id`, email)
		if set != "" {
			if _, err := conn.ExecContext(ctx, `UPDATE users SET `+set+` WHERE id = $1`, id); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	paying := user("paying@example.invalid", `subscription_valid = true`)
	comped := user("comped@example.invalid", `host_comp_until = NOW() + INTERVAL '7 days', host_comp_since = NOW()`)
	zoneComped := user("zone@example.invalid", `host_comp_until = NOW() + INTERVAL '1 day', host_comp_since = NOW()`)
	lapsed := user("lapsed@example.invalid", `host_comp_until = NOW() - INTERVAL '1 minute'`)
	free := user("free@example.invalid", "")

	systemID := mustID(t, conn, `INSERT INTO systems (system_id, user_id, name, api_key, frequency, steal_capcodes, approved_at, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, 'rx', 'key-rx', 1, '0', NOW(), NOW(), NOW()) RETURNING id`, paying)
	capcode := mustID(t, conn, `INSERT INTO capcodes (address, alias, agency, icon, colour, "ignore") VALUES ('1310104', 'Central', 'Lower Hunter', 'fire', 'red', false) RETURNING id`)
	for _, u := range []int64{paying, comped, lapsed, free} {
		if _, err := conn.ExecContext(ctx, `INSERT INTO subscriptions (user_id, capcode_id, created_at, updated_at) VALUES ($1, $2, NOW(), NOW())`, u, capcode); err != nil {
			t.Fatal(err)
		}
	}
	for _, u := range []int64{zoneComped, lapsed, free} {
		if _, err := conn.ExecContext(ctx, `INSERT INTO alertzones (user_id, name, lat, lng, radius, created_at, updated_at) VALUES ($1, 'home', -32.83, 151.35, 10, NOW(), NOW())`, u); err != nil {
			t.Fatal(err)
		}
	}
	msg := mustID(t, conn, `INSERT INTO messages (system_id, capcode_id, address, message, md5, lat, lng, created_at, updated_at)
		VALUES ($1, $2, '1310104', 'STRUCTURE FIRE CESSNOCK', md5('x'), '-32.83', '151.35', NOW(), NOW()) RETURNING id`, systemID, capcode)

	if n := fanOut(t, conn, msg); n != 3 {
		t.Fatalf("fan-out notified %d users, want 3 (paying, comped, zone comped)", n)
	}
	var notified []int64
	if err := conn.SelectContext(ctx, &notified, `SELECT notifiable_id FROM notifications ORDER BY notifiable_id`); err != nil {
		t.Fatal(err)
	}
	want := map[int64]bool{paying: true, comped: true, zoneComped: true}
	for _, id := range notified {
		if !want[id] {
			t.Fatalf("notified %v; lapsed %d / free %d must not be", notified, lapsed, free)
		}
		delete(want, id)
	}
	if len(want) != 0 {
		t.Fatalf("not notified: %v", want)
	}
}
