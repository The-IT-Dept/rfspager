package dispatch

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/db"
)

// fanoutTestDB migrates a fresh schema on RFSPAGER_TEST_DATABASE_URL, like
// the db package's integration tests, and returns the raw connection the
// fan-out worker uses.
func fanoutTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	rawURL := os.Getenv("RFSPAGER_TEST_DATABASE_URL")
	if rawURL == "" {
		t.Skip("RFSPAGER_TEST_DATABASE_URL is not set")
	}
	u, err := url.Parse(rawURL)
	if err != nil || !strings.HasPrefix(strings.TrimPrefix(u.Path, "/"), "rfspager_test") {
		t.Fatalf("RFSPAGER_TEST_DATABASE_URL must name a rfspager_test* database")
	}
	d, err := db.New(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := d.Conn().ExecContext(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return d.Conn()
}

func mustID(t *testing.T, conn *sqlx.DB, query string, args ...any) int64 {
	t.Helper()
	var id int64
	if err := conn.QueryRowContext(context.Background(), query, args...).Scan(&id); err != nil {
		t.Fatalf("%s: %v", query[:40], err)
	}
	return id
}

func fanOut(t *testing.T, conn *sqlx.DB, messageID int64) int {
	t.Helper()
	rows, err := conn.QueryxContext(context.Background(), fanoutSQL, fanoutLockKey, messageID)
	if err != nil {
		t.Fatalf("fanout %d: %v", messageID, err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	return n
}

// A user following several capcodes is notified once per incident message:
// copies of the same dispatch to the other capcodes within 15 minutes are
// not pushed, a different message for the same incident is, and the same
// incident key outside the window is again.
func TestFanoutNotifiesOncePerIncidentPerUser(t *testing.T) {
	conn := fanoutTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	userID := mustID(t, conn, `INSERT INTO users (user_id, email, password, subscription_valid) VALUES (gen_random_uuid(), 'sub@example.invalid', '!', true) RETURNING id`)
	systemID := mustID(t, conn, `INSERT INTO systems (system_id, user_id, name, api_key, frequency, steal_capcodes, approved_at, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, 'rx', 'key-rx', 1, '0', NOW(), NOW(), NOW()) RETURNING id`, userID)
	capA := mustID(t, conn, `INSERT INTO capcodes (address, alias, agency, icon, colour, "ignore") VALUES ('1310104', 'Central', 'Lower Hunter', 'fire', 'red', false) RETURNING id`)
	capB := mustID(t, conn, `INSERT INTO capcodes (address, alias, agency, icon, colour, "ignore") VALUES ('1310176', 'Neath', 'Lower Hunter', 'fire', 'red', false) RETURNING id`)
	for _, c := range []int64{capA, capB} {
		if _, err := conn.ExecContext(ctx, `INSERT INTO subscriptions (user_id, capcode_id, created_at, updated_at) VALUES ($1, $2, NOW(), NOW())`, userID, c); err != nil {
			t.Fatal(err)
		}
	}
	insertMsg := func(cap int64, address, text, key string, at time.Time) int64 {
		return mustID(t, conn, `INSERT INTO messages (system_id, capcode_id, address, message, md5, incident_key, created_at, updated_at)
			VALUES ($1, $2, $3, $4, md5($4), $5, $6, $6) RETURNING id`, systemID, cap, address, text, key, at)
	}
	call := "26-131320 - Electrical appliance fire - STRUCTURE FIRE - OPP 21 LOWER ST,CESSNOCK"
	m1 := insertMsg(capA, "1310104", "13 October 2026 22:38:32 LHCENTR1 - "+call, "rfs:26-131320:call", now)
	m2 := insertMsg(capB, "1310176", "13 October 2026 22:42:13 LHNEATH7 - "+call, "rfs:26-131320:call", now.Add(4*time.Minute))
	m3 := insertMsg(capA, "1310104", "13 October 2026 23:10:02 26-131320 LHCENTR1 Stop Message // CESSNOCK", "rfs:26-131320:stop", now.Add(6*time.Minute))
	m4 := insertMsg(capB, "1310176", "13 October 2026 23:30:00 LHNEATH7 - "+call, "rfs:26-131320:call", now.Add(40*time.Minute))
	m5 := insertMsg(capB, "1310176", "13 October 2026 22:45:00 LHNEATH7 - older format page", "", now.Add(7*time.Minute))

	if n := fanOut(t, conn, m1); n != 1 {
		t.Fatalf("first copy should notify the user, got %d", n)
	}
	if n := fanOut(t, conn, m2); n != 0 {
		t.Fatalf("same incident to the second capcode should be suppressed, got %d", n)
	}
	if n := fanOut(t, conn, m3); n != 1 {
		t.Fatalf("the stop message is a different message and should notify, got %d", n)
	}
	if n := fanOut(t, conn, m4); n != 1 {
		t.Fatalf("the same key outside the window should notify again, got %d", n)
	}
	if n := fanOut(t, conn, m5); n != 1 {
		t.Fatalf("a message without an incident key is never suppressed, got %d", n)
	}
	// Re-running a message is idempotent.
	if n := fanOut(t, conn, m1); n != 0 {
		t.Fatalf("re-fanout inserted %d duplicate notifications", n)
	}
	var total int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM notifications WHERE notifiable_id = $1`, userID).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 4 {
		t.Fatalf("expected 4 notifications for the user, got %d", total)
	}
}
