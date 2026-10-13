package admin

import (
	"context"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/the-it-dept/rfspager/internal/db"
)

// messageDetailTestDB migrates a fresh schema on RFSPAGER_TEST_DATABASE_URL,
// like the db and dispatch integration tests.
func messageDetailTestDB(t *testing.T) *sqlx.DB {
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

func insertID(t *testing.T, conn *sqlx.DB, query string, args ...any) int64 {
	t.Helper()
	var id int64
	if err := conn.QueryRowContext(context.Background(), query, args...).Scan(&id); err != nil {
		t.Fatalf("%s: %v", query[:40], err)
	}
	return id
}

func mustExec(t *testing.T, conn *sqlx.DB, query string, args ...any) {
	t.Helper()
	if _, err := conn.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("%s: %v", query[:40], err)
	}
}

// The detail endpoints behind the admin message page: every receiver's copy
// of the page with the kept one marked, the other messages of the same
// incident inside the suppression window, and who was pushed with what.
func TestMessageDetailQueries(t *testing.T) {
	conn := messageDetailTestDB(t)
	ctx := context.Background()
	store := NewStore(conn)
	at := time.Date(2026, 10, 13, 14, 55, 4, 0, time.UTC)

	userA := insertID(t, conn, `INSERT INTO users (user_id, email, password, subscription_valid) VALUES (gen_random_uuid(), 'a@example.invalid', '!', true) RETURNING id`)
	userB := insertID(t, conn, `INSERT INTO users (user_id, email, password, subscription_valid) VALUES (gen_random_uuid(), 'b@example.invalid', '!', true) RETURNING id`)
	newSystem := func(name string) int64 {
		return insertID(t, conn, `INSERT INTO systems (system_id, user_id, name, api_key, frequency, steal_capcodes, approved_at, created_at, updated_at)
			VALUES (gen_random_uuid(), $1, $2, $2, 1, '0', NOW(), NOW(), NOW()) RETURNING id`, userA, name)
	}
	budgewoi := newSystem("Budgewoi")
	newcastle := newSystem("Newcastle")
	capA := insertID(t, conn, `INSERT INTO capcodes (address, alias, agency, icon, colour, "ignore") VALUES ('0950048', 'Hunter A', 'Hunter Valley', 'fire', 'red', false) RETURNING id`)
	capB := insertID(t, conn, `INSERT INTO capcodes (address, alias, agency, icon, colour, "ignore") VALUES ('1170040', 'Hunter B', 'Hunter Valley', 'fire', 'red', false) RETURNING id`)

	clean := "14 October 2026 01:54:54 HVDO - 26-131331 - Assist Ambulance - ASSIST AMBOS - 551 SANDY CREEK RD"
	garbled := "14 October 2026 01:54:54 HVDO 5 \"T-131331 - Assest Ambulance\"d ASSI"
	const key = "rfs:26-131331:3afcd40390e49e15"
	newMessage := func(system, cap int64, address, text, incidentKey string, when time.Time) int64 {
		return insertID(t, conn, `INSERT INTO messages (system_id, capcode_id, address, message, md5, quality, incident_key, replaced_count, created_at, updated_at)
			VALUES ($1, $2, $3, $4, md5($4), '{"codewords":56,"corrected":14,"uncorrectable":0}', NULLIF($5, ''), 1, $6, $6) RETURNING id`,
			system, cap, address, text, incidentKey, when)
	}
	m1 := newMessage(newcastle, capA, "0950048", clean, key, at)
	sibling := newMessage(newcastle, capB, "1170040", clean, key, at.Add(9*time.Second))
	newMessage(newcastle, capB, "1170040", clean, key, at.Add(20*time.Minute))                    // outside the window
	newMessage(newcastle, capB, "1170040", "other job", "rfs:26-131399:x", at.Add(5*time.Second)) // different incident
	deleted := newMessage(newcastle, capB, "1170040", clean, key, at.Add(10*time.Second))
	mustExec(t, conn, `UPDATE messages SET deleted_at = NOW() WHERE id = $1`, deleted)
	noKey := newMessage(budgewoi, capA, "0950048", "older format page", "", at.Add(time.Hour))

	pageLog := func(system int64, when time.Time, raw string) {
		mustExec(t, conn, `INSERT INTO system_logs (system_id, ts, received_at, level, source, message, raw) VALUES ($1, $2, $2, 'info', 'receiver', 'page', $3::jsonb)`, system, when, raw)
	}
	pageLog(budgewoi, at.Add(-time.Second), `{"channel":"RFS","address":"0950048","corrected":31,"uncorrectable":3,"truncated":false,"msg":`+strconv.Quote(garbled)+`}`)
	pageLog(newcastle, at.Add(300*time.Millisecond), `{"channel":"RFS","address":"0950048","corrected":14,"uncorrectable":0,"msg":`+strconv.Quote(clean)+`}`)
	pageLog(budgewoi, at.Add(-20*time.Minute), `{"channel":"RFS","address":"0950048","msg":"an earlier page to the same capcode"}`)
	pageLog(budgewoi, at, `{"channel":"RFS","address":"1170040","msg":"a page to another capcode"}`)
	mustExec(t, conn, `INSERT INTO system_logs (system_id, ts, received_at, level, source, message, raw) VALUES ($1, $2, $2, 'info', 'receiver', 'stats', '{"address":"0950048"}')`, budgewoi, at)

	notify := func(user, message int64, data string, when time.Time, pushed, failed *time.Time, attempts int, lastErr *string) {
		mustExec(t, conn, `INSERT INTO notifications (id, type, notifiable_type, notifiable_id, data, message_id, created_at, updated_at, push_required, pushed_at, push_failed_at, push_attempts, push_last_error)
			VALUES (gen_random_uuid(), 'App\Notifications\PagerNotification', 'App\Models\User', $1, $2, $3, $4, $4, true, $5, $6, $7, $8)`,
			user, data, message, when, pushed, failed, attempts, lastErr)
	}
	pushedAt := at.Add(time.Second)
	failedAt := at.Add(2 * time.Second)
	lastErr := "device token unregistered"
	payload := func(id int64, text string) string {
		return `{"id": ` + strconv.FormatInt(id, 10) + `, "message": ` + strconv.Quote(text) + `}`
	}
	notify(userA, m1, payload(m1, garbled), at, &pushedAt, nil, 1, nil)
	notify(userB, m1, payload(m1, garbled), at.Add(time.Second), nil, &failedAt, 3, &lastErr)
	notify(userA, sibling, payload(sibling, clean), at.Add(9*time.Second), &pushedAt, nil, 1, nil)

	t.Run("copies", func(t *testing.T) {
		copies, err := store.ListMessageCopies(ctx, m1)
		if err != nil {
			t.Fatal(err)
		}
		if len(copies) != 2 {
			t.Fatalf("expected the two decodes of the page, got %d: %v", len(copies), copies)
		}
		first, second := copies[0], copies[1]
		if first["system_name"] != "Budgewoi" || first["kept"] != false || first["corrected"] != int64(31) || first["uncorrectable"] != int64(3) {
			t.Fatalf("first copy = %v", first)
		}
		if second["system_name"] != "Newcastle" || second["kept"] != true || second["message"] != clean || second["truncated"] != false {
			t.Fatalf("second copy = %v", second)
		}
	})

	t.Run("incident", func(t *testing.T) {
		rows, err := store.ListIncidentMessages(ctx, m1)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0]["id"] != sibling {
			t.Fatalf("expected only the in-window sibling %d, got %v", sibling, rows)
		}
		if rows[0]["capcode_alias"] != "Hunter B" || rows[0]["notifications_count"] != int64(1) || rows[0]["notifications_pushed_count"] != int64(1) {
			t.Fatalf("sibling = %v", rows[0])
		}
		if _, ok := rows[0]["quality"].(map[string]any); !ok {
			t.Fatalf("quality should decode as JSON, got %T", rows[0]["quality"])
		}
		none, err := store.ListIncidentMessages(ctx, noKey)
		if err != nil {
			t.Fatal(err)
		}
		if len(none) != 0 {
			t.Fatalf("a message without an incident key has no siblings, got %v", none)
		}
	})

	t.Run("notifications", func(t *testing.T) {
		out, err := store.ListMessageNotifications(ctx, m1, 200)
		if err != nil {
			t.Fatal(err)
		}
		if out.Total != 2 || out.Pushed != 1 || out.Failed != 1 || len(out.Data) != 2 {
			t.Fatalf("totals = %+v", out)
		}
		newest, oldest := out.Data[0], out.Data[1]
		if newest["user_email"] != "b@example.invalid" || newest["push_failed_at"] == nil || newest["push_attempts"] != int64(3) || newest["push_last_error"] != lastErr {
			t.Fatalf("failed push row = %v", newest)
		}
		if oldest["user_email"] != "a@example.invalid" || oldest["pushed_at"] == nil || oldest["pushed_message"] != garbled {
			t.Fatalf("pushed row = %v", oldest)
		}
		capped, err := store.ListMessageNotifications(ctx, m1, 1)
		if err != nil {
			t.Fatal(err)
		}
		if capped.Total != 2 || len(capped.Data) != 1 {
			t.Fatalf("limit should cap rows but not totals: %+v", capped)
		}
	})
}
