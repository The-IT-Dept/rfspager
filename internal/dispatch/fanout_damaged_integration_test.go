package dispatch

import (
	"context"
	"testing"
	"time"
)

// A heavily repaired copy that reaches fan-out as its own message (its
// damage having slipped past the dedupe) is not pushed when another page
// went to the same capcode in the previous three minutes: it is a re-send,
// and its readers already have the page. Clean pages, damaged pages with no
// recent neighbour, and rows with no quality record are unaffected.
func TestFanoutDoesNotWakeUsersForDamagedResend(t *testing.T) {
	conn := fanoutTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	userID := mustID(t, conn, `INSERT INTO users (user_id, email, password, subscription_valid) VALUES (gen_random_uuid(), 'sub@example.invalid', '!', true) RETURNING id`)
	systemID := mustID(t, conn, `INSERT INTO systems (system_id, user_id, name, api_key, frequency, steal_capcodes, approved_at, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, 'rx', 'key-rx', 1, '0', NOW(), NOW(), NOW()) RETURNING id`, userID)
	cap := mustID(t, conn, `INSERT INTO capcodes (address, alias, agency, icon, colour, "ignore") VALUES ('0010864', 'Raymond Terrace', 'Lower Hunter', 'fire', 'red', false) RETURNING id`)
	if _, err := conn.ExecContext(ctx, `INSERT INTO subscriptions (user_id, capcode_id, created_at, updated_at) VALUES ($1, $2, NOW(), NOW())`, userID, cap); err != nil {
		t.Fatal(err)
	}
	insert := func(text, key string, quality *string, at time.Time) int64 {
		return mustID(t, conn, `INSERT INTO messages (system_id, capcode_id, address, message, md5, incident_key, quality, created_at, updated_at)
			VALUES ($1, $2, '0010864', $3, md5($3), $4, $5::jsonb, $6, $6) RETURNING id`, systemID, cap, text, key, quality, at)
	}
	str := func(s string) *string { return &s }
	clean := str(`{"codewords":54,"corrected":9,"uncorrectable":0}`)
	heavy := str(`{"codewords":54,"corrected":40,"uncorrectable":0}`)
	oneBad := str(`{"codewords":54,"corrected":34,"uncorrectable":1}`)

	first := insert("14 October 2026 07:46:03 LHRAYTE - 26-131349 - Power pole alight", "rfs:26-131349:a", clean, now)
	resend := insert("14 October 2026 07:46:03 LHRaYE - 26-131349 - Power pole alight", "rfs:26-131349:b", heavy, now.Add(40*time.Second))
	resend2 := insert("14 October 2026 07:46:04 LHRAYTE1 - 26-1149 - Poer polG `light", "msg:c", oneBad, now.Add(52*time.Second))
	cleanNext := insert("14 October 2026 07:47:30 LHRAYTE - 26-131350 - Grass fire", "rfs:26-131350:d", clean, now.Add(80*time.Second))
	lateDamaged := insert("14 October 2026 07:58:12 26-131349 LHRAYTE Stop Message", "rfs:26-131349:e", oneBad, now.Add(12*time.Minute))
	legacy := insert("14 October 2026 08:00:00 LHRAYTE - 26-131351 - Smoke sighting", "rfs:26-131351:f", nil, now.Add(13*time.Minute))

	for _, c := range []struct {
		name string
		id   int64
		want int
	}{
		{"clean first page", first, 1},
		{"heavily corrected re-send 40 s later", resend, 0},
		{"unrepairable re-send 52 s later", resend2, 0},
		{"a clean different page a minute later", cleanNext, 1},
		{"a damaged page with no neighbour in three minutes", lateDamaged, 1},
		{"a row without a quality record", legacy, 1},
	} {
		if n := fanOut(t, conn, c.id); n != c.want {
			t.Fatalf("%s: notified %d users, want %d", c.name, n, c.want)
		}
	}
}
