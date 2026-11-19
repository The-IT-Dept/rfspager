package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/the-it-dept/rfspager/internal/models"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

func pageAt(systemID int64, capcode sql.NullInt64, address, text string, at time.Time, q *rxproto.PageQuality) *models.Message {
	return &models.Message{SystemID: systemID, CapcodeID: capcode, Address: address, Message: text, CreatedAt: at, Quality: q}
}

func mustUpsert(t *testing.T, d *DB, m *models.Message, want string) {
	t.Helper()
	out, err := d.UpsertMessageDeduped(context.Background(), m)
	if err != nil || out != want {
		t.Fatalf("upsert %.40q: %s %v, want %s", m.Message, out, err, want)
	}
}

// An FRNSW turnout (17 Nov 2026): one receiver's truncated, repaired copy was
// stored a second before the clean copies of the same transmission. The
// clean copy now replaces it instead of becoming a second page.
func TestUpsertMergesCleanCopyIntoDamagedFragmentStoredFirst(t *testing.T) {
	d, sysA := managedSystemDB(t)
	sysB := seedManagedSystem(t, d, "other")
	capcode := seedCapcode(t, d, "0100011", "101 Example")
	at := time.Now().UTC().Truncate(time.Second)

	fragment := pageAt(sysA, capcode, "0100011", "LZmNC TYPE: AFA TURNOUT: ", at,
		&rxproto.PageQuality{Codewords: 9, Corrected: 4, Uncorrectable: 1, Truncated: true, Damaged: [][2]int{{0, 2}}})
	mustUpsert(t, d, fragment, MessageCreated)
	clean := pageAt(sysB, capcode, "0100011", "FRINC TYPE: AFA TURNOUT: 101 INC: 100001-17112026", at.Add(time.Second),
		&rxproto.PageQuality{Codewords: 18})
	mustUpsert(t, d, clean, MessageReplaced)
	if clean.ID != fragment.ID {
		t.Fatalf("replaced row %d, want %d", clean.ID, fragment.ID)
	}
	if text, _, _ := storedMessage(t, d, fragment.ID); text != clean.Message {
		t.Fatalf("stored %q", text)
	}

	// A different incident to the same station a minute later is its own page.
	other := pageAt(sysB, capcode, "0100011", "FRINC TYPE: AFA TURNOUT: 101 INC: 100009-17112026", at.Add(time.Minute),
		&rxproto.PageQuality{Codewords: 18})
	mustUpsert(t, d, other, MessageCreated)
}

// A follow-up page to the same brigade for the same incident, with its own
// dispatch time, is not folded into a truncated copy of the first page.
func TestUpsertKeepsFollowUpPageApartFromTruncatedCopy(t *testing.T) {
	d, sys := managedSystemDB(t)
	capcode := seedCapcode(t, d, "1310104", "Central")
	at := time.Now().UTC().Truncate(time.Second)

	mustUpsert(t, d, pageAt(sys, capcode, "1310104", "13 October 2026 22:38:32 LHCENTR1 - 26-100040 - El", at,
		&rxproto.PageQuality{Codewords: 18, Corrected: 2, Truncated: true}), MessageCreated)
	mustUpsert(t, d, pageAt(sys, capcode, "1310104",
		"13 October 2026 22:40:10 LHCENTR1 - 26-100040 - Electrical appliance fire - STRUCTURE FIRE - 1 EXAMPLE ST - 2ND ALARM",
		at.Add(98*time.Second), &rxproto.PageQuality{Codewords: 48}), MessageCreated)
}

// The paging network re-sends a page it dispatched a day ago. Its header
// (dispatch time to the second plus unit code) finds the original up to
// 72 hours back.
func TestUpsertMatchesReplayToOriginalWithin72Hours(t *testing.T) {
	d, sys := managedSystemDB(t)
	capcode := seedCapcode(t, d, "1310998", "Example DO")
	now := time.Now().UTC().Truncate(time.Second)
	text := "18 November 2026 22:14:14 LHDO - 26-100008 - Car fire - VEHICLE FIRE - EXAMPLE DR"

	original := pageAt(sys, capcode, "1310998", text, now.Add(-15*time.Hour), &rxproto.PageQuality{Codewords: 48})
	mustUpsert(t, d, original, MessageCreated)
	replay := pageAt(sys, capcode, "1310998", text, now, &rxproto.PageQuality{Codewords: 48})
	mustUpsert(t, d, replay, MessageDuplicate)

	// A replay with a few repaired characters after the header still matches.
	damaged := pageAt(sys, capcode, "1310998", "18 November 2026 22:14:14 LHDO - 26-100008 - Car firE - VEHICLE FIRE - EXAMPLE DR",
		now.Add(5*time.Second), &rxproto.PageQuality{Codewords: 48, Corrected: 6})
	mustUpsert(t, d, damaged, MessageDuplicate)

	// Older than 72 hours: a new row (fan-out's replay rule decides the push).
	old := "15 November 2026 10:00:00 LHDO - 26-100001 - Grass - FIRECALL - EXAMPLE RD"
	mustUpsert(t, d, pageAt(sys, capcode, "1310998", old, now.Add(-80*time.Hour), &rxproto.PageQuality{Codewords: 40}), MessageCreated)
	mustUpsert(t, d, pageAt(sys, capcode, "1310998", old, now, &rxproto.PageQuality{Codewords: 40}), MessageCreated)
}

// During a replay one receiver's copy with a damaged header is stored first
// (nothing matches it); a clean copy of the same replay a second later must
// repair that copy, not just match the day-old original, or the damaged
// copy would be pushed after its hold.
func TestUpsertRepairsDamagedReplayCopyBeforeMatchingTheOriginal(t *testing.T) {
	d, sys := managedSystemDB(t)
	capcode := seedCapcode(t, d, "1310144", "Example")
	now := time.Now().UTC().Truncate(time.Second)
	text := "18 November 2026 22:14:13 LHEXAMP - 26-100008 - Car fire - VEHICLE FIRE - EXAMPLE DR,EXAMPLEVILLE"

	original := pageAt(sys, capcode, "1310144", text, now.Add(-15*time.Hour), &rxproto.PageQuality{Codewords: 50})
	mustUpsert(t, d, original, MessageCreated)
	damaged := pageAt(sys, capcode, "1310144", "18 November 206f 22:14:13 LHEXAMP - 2>-100008 - Car firE - VEHICLE FIRE - EXAMPLE DR,EXAMPLEVILLE",
		now, &rxproto.PageQuality{Codewords: 50, Corrected: 20, Uncorrectable: 2, Damaged: [][2]int{{14, 17}, {38, 41}}})
	mustUpsert(t, d, damaged, MessageCreated)
	clean := pageAt(sys, capcode, "1310144", text, now.Add(time.Second), &rxproto.PageQuality{Codewords: 50})
	mustUpsert(t, d, clean, MessageReplaced)
	if clean.ID != damaged.ID {
		t.Fatalf("clean replay copy replaced row %d, want the damaged copy %d", clean.ID, damaged.ID)
	}
}

type notifState struct {
	data        map[string]any
	pushedAt    sql.NullTime
	failedAt    sql.NullTime
	attempts    int
	lockedUntil sql.NullTime
}

func readNotif(t *testing.T, d *DB, id string) notifState {
	t.Helper()
	var raw string
	var s notifState
	if err := d.conn.QueryRowContext(context.Background(),
		`SELECT data, pushed_at, push_failed_at, push_attempts, push_locked_until FROM notifications WHERE id = $1`, id).
		Scan(&raw, &s.pushedAt, &s.failedAt, &s.attempts, &s.lockedUntil); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(raw), &s.data); err != nil {
		t.Fatal(err)
	}
	return s
}

// A better copy that replaces a message's text updates each of its pushes by
// where that push is: carried to unsent ones and pending corrections, carried
// unmarked to one in flight (the dispatcher decides), a correction queued
// for one delivered recently; left alone when delivered long ago, already
// corrected, or failed. Fan-out is reopened so the clean copy decides again.
func TestReplacementCorrectsPushesByState(t *testing.T) {
	d, sys := managedSystemDB(t)
	ctx := context.Background()
	capcode := seedCapcode(t, d, "0100211", "102 Example")
	now := time.Now().UTC().Truncate(time.Second)
	damagedText := "FRINC TYPE: q^C TURNOUT: 102 IC: 100002-17112P26"
	cleanText := "FRINC TYPE: MVA TURNOUT: 102 INC: 100002-17112026"

	first := pageAt(sys, capcode, "0100211", damagedText, now,
		&rxproto.PageQuality{Codewords: 18, Corrected: 6, Uncorrectable: 2, Damaged: [][2]int{{12, 15}, {33, 36}}})
	mustUpsert(t, d, first, MessageCreated)
	if _, err := d.conn.ExecContext(ctx, `UPDATE messages SET fanned_out_at = NOW() - interval '25 seconds' WHERE id = $1`, first.ID); err != nil {
		t.Fatal(err)
	}

	data := func(extra string) string {
		base := `{"id": ` + itoa(first.ID) + `, "title": "Fire and Rescue NSW | 102 Example", "message": "` + damagedText + `", "body": "` + damagedText + `"`
		return base + extra + `}`
	}
	insert := func(name, payload string, sets string) string {
		var userID int64
		if err := d.conn.QueryRowContext(ctx, `INSERT INTO users (user_id, email, password) VALUES (gen_random_uuid(), $1, '!') RETURNING id`,
			name+"@example.invalid").Scan(&userID); err != nil {
			t.Fatal(err)
		}
		var id string
		if err := d.conn.QueryRowContext(ctx,
			`INSERT INTO notifications (id, type, notifiable_type, notifiable_id, data, message_id, push_required, created_at, updated_at)
			 VALUES (gen_random_uuid(), 'App\Notifications\PagerNotification', 'App\Models\User', $1, $2, $3, true,
			         NOW() - interval '25 seconds', NOW()) RETURNING id::text`,
			userID, payload, first.ID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if sets != "" {
			if _, err := d.conn.ExecContext(ctx, `UPDATE notifications SET `+sets+` WHERE id = $1::uuid`, id); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	unsent := insert("unsent", data(""), "")
	inFlight := insert("inflight", data(""), "push_attempts = 1, push_locked_until = NOW() + interval '2 minutes'")
	delivered := insert("delivered", data(""), "push_attempts = 1, pushed_at = NOW() - interval '20 seconds'")
	longAgo := insert("longago", data(""), "push_attempts = 1, pushed_at = NOW() - interval '20 minutes', created_at = NOW() - interval '20 minutes'")
	corrected := insert("corrected", data(`, "correction": true, "correction_body": "earlier fix", "first_pushed_at": "2026-11-17T01:33:07Z"`), "push_attempts = 1, pushed_at = NOW() - interval '5 seconds'")
	failed := insert("failed", data(""), "push_attempts = 5, push_failed_at = NOW()")
	pending := insert("pending", data(`, "correction": true, "correction_body": "earlier fix", "first_pushed_at": "2026-11-17T01:33:07Z"`), "")

	better := pageAt(sys, capcode, "0100211", cleanText, now.Add(time.Second), &rxproto.PageQuality{Codewords: 18})
	mustUpsert(t, d, better, MessageReplaced)

	s := readNotif(t, d, unsent)
	if s.data["body"] != cleanText || s.data["correction"] != nil {
		t.Errorf("unsent: %v", s.data)
	}
	s = readNotif(t, d, inFlight)
	if s.data["body"] != cleanText || s.data["correction"] != nil || s.attempts != 1 || s.pushedAt.Valid || !s.lockedUntil.Valid {
		t.Errorf("in flight: data %v attempts %d pushed %v locked %v", s.data, s.attempts, s.pushedAt, s.lockedUntil)
	}
	s = readNotif(t, d, delivered)
	if s.data["body"] != damagedText || s.data["correction"] != true || s.data["correction_body"] != cleanText ||
		s.data["first_pushed_at"] == nil || s.pushedAt.Valid || s.attempts != 0 {
		t.Errorf("delivered: data %v attempts %d pushed %v", s.data, s.attempts, s.pushedAt)
	}
	s = readNotif(t, d, longAgo)
	if s.data["body"] != damagedText || s.data["correction"] != nil || !s.pushedAt.Valid {
		t.Errorf("delivered long ago: %v", s.data)
	}
	s = readNotif(t, d, corrected)
	if s.data["correction_body"] != "earlier fix" || !s.pushedAt.Valid {
		t.Errorf("already corrected: %v", s.data)
	}
	s = readNotif(t, d, failed)
	if s.data["body"] != damagedText || !s.failedAt.Valid {
		t.Errorf("failed: %v", s.data)
	}
	s = readNotif(t, d, pending)
	if s.data["correction_body"] != cleanText || s.data["body"] != damagedText {
		t.Errorf("pending correction: %v", s.data)
	}

	var fanned sql.NullTime
	if err := d.conn.QueryRowContext(ctx, `SELECT fanned_out_at FROM messages WHERE id = $1`, first.ID).Scan(&fanned); err != nil {
		t.Fatal(err)
	}
	if fanned.Valid {
		t.Error("an undamaged replacement soon after fan-out must reopen it")
	}
}

// A replacement that is better but still damaged carries its text to pushes
// not yet tried, but corrects nothing and leaves fan-out alone.
func TestDamagedReplacementCorrectsNothing(t *testing.T) {
	d, sys := managedSystemDB(t)
	ctx := context.Background()
	capcode := seedCapcode(t, d, "0100311", "103 Example")
	now := time.Now().UTC().Truncate(time.Second)

	first := pageAt(sys, capcode, "0100311", "FRINC TYPE: q^C TURNOUT: 103 IC: 100003-17112P26", now,
		&rxproto.PageQuality{Codewords: 18, Corrected: 6, Uncorrectable: 3, Damaged: [][2]int{{12, 15}}})
	mustUpsert(t, d, first, MessageCreated)
	if _, err := d.conn.ExecContext(ctx, `UPDATE messages SET fanned_out_at = NOW() WHERE id = $1`, first.ID); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err := d.conn.QueryRowContext(ctx, `INSERT INTO users (user_id, email, password) VALUES (gen_random_uuid(), 'u@example.invalid', '!') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := d.conn.QueryRowContext(ctx,
		`INSERT INTO notifications (id, type, notifiable_type, notifiable_id, data, message_id, push_required, pushed_at, push_attempts, created_at, updated_at)
		 VALUES (gen_random_uuid(), 'App\Notifications\PagerNotification', 'App\Models\User', $1, $2, $3, true, NOW(), 1, NOW(), NOW())
		 RETURNING id::text`,
		userID, `{"id": 1, "message": "x", "body": "x"}`, first.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}

	better := pageAt(sys, capcode, "0100311", "FRINC TYPE: MVA TURNOUT: 103 IC: 100003-17112026", now.Add(time.Second),
		&rxproto.PageQuality{Codewords: 18, Corrected: 3, Uncorrectable: 1, Damaged: [][2]int{{30, 33}}})
	mustUpsert(t, d, better, MessageReplaced)
	if s := readNotif(t, d, id); s.data["correction"] != nil || !s.pushedAt.Valid {
		t.Errorf("a still-damaged copy must not send a correction: %v", s.data)
	}
	var fanned sql.NullTime
	if err := d.conn.QueryRowContext(ctx, `SELECT fanned_out_at FROM messages WHERE id = $1`, first.ID).Scan(&fanned); err != nil {
		t.Fatal(err)
	}
	if !fanned.Valid {
		t.Error("a still-damaged copy must not reopen fan-out")
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
