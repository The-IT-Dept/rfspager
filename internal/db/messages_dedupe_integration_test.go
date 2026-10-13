package db

import (
	"context"
	"testing"
	"time"

	"github.com/the-it-dept/rfspager/internal/models"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

const (
	fullPage      = "13 October 2026 22:47:41 LHPOKOL1 - 26-131320 - Electrical appliance fire - STRUCTURE FIRE - OPP 21 LOWER ST,CESSNOCK,CESSNOCK (NSW),2325 - [151.35,-32.83]"
	truncatedPage = "13 October 2026 22:47:41 LHPOKOL1 - 26-131320 - Electrical appliance fire - STRUCTURE FI"
	garbledPage   = "13 October 2026 22:47:41 LHPOKOL1 - 26-123#0 - Electriz!l appliance fire - STSUBTURE VISD$\r`O@P("
)

func newPage(systemID int64, text string, at time.Time, q *rxproto.PageQuality) *models.Message {
	return &models.Message{SystemID: systemID, Address: "1310128", Message: text, CreatedAt: at, Quality: q, IncidentKey: "rfs:26-131320:x"}
}

func storedMessage(t *testing.T, d *DB, id int64) (text string, replaced int, system int64) {
	t.Helper()
	if err := d.conn.QueryRowContext(context.Background(),
		`SELECT message, replaced_count, system_id FROM messages WHERE id = $1`, id).Scan(&text, &replaced, &system); err != nil {
		t.Fatal(err)
	}
	return
}

// A truncated copy stored first is replaced by the complete copy that a
// second receiver hears seconds later; the row keeps its id.
func TestUpsertReplacesTruncatedCopyWithCompleteOne(t *testing.T) {
	d, sysA := managedSystemDB(t)
	sysB := seedManagedSystem(t, d, "other")
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	first := newPage(sysA, truncatedPage, now, &rxproto.PageQuality{Codewords: 22, Corrected: 22, Truncated: true})
	if out, err := d.UpsertMessageDeduped(ctx, first); err != nil || out != MessageCreated {
		t.Fatalf("first: %s %v", out, err)
	}
	second := newPage(sysB, fullPage, now.Add(2*time.Second), &rxproto.PageQuality{Codewords: 40, Corrected: 21})
	out, err := d.UpsertMessageDeduped(ctx, second)
	if err != nil || out != MessageReplaced || second.ID != first.ID {
		t.Fatalf("second: out=%s err=%v id=%d want %d", out, err, second.ID, first.ID)
	}
	text, replaced, system := storedMessage(t, d, first.ID)
	if text != fullPage || replaced != 1 || system != sysB {
		t.Fatalf("stored = %q replaced=%d system=%d", text, replaced, system)
	}
	// A third, equally good copy is just a duplicate.
	third := newPage(sysA, fullPage, now.Add(9*time.Second), &rxproto.PageQuality{Codewords: 40, Corrected: 2})
	if out, err := d.UpsertMessageDeduped(ctx, third); err != nil || out != MessageDuplicate {
		t.Fatalf("third: %s %v", out, err)
	}
}

// A clean copy stored first is kept when a garbled copy arrives.
func TestUpsertKeepsCleanCopyOverGarbled(t *testing.T) {
	d, sysA := managedSystemDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	clean := newPage(sysA, fullPage, now, &rxproto.PageQuality{Codewords: 40})
	if out, err := d.UpsertMessageDeduped(ctx, clean); err != nil || out != MessageCreated {
		t.Fatalf("clean: %s %v", out, err)
	}
	garbled := newPage(sysA, garbledPage, now.Add(time.Second), &rxproto.PageQuality{Codewords: 40, Corrected: 37, Uncorrectable: 12})
	if out, err := d.UpsertMessageDeduped(ctx, garbled); err != nil || out != MessageDuplicate {
		t.Fatalf("garbled: %s %v", out, err)
	}
	if text, _, _ := storedMessage(t, d, clean.ID); text != fullPage {
		t.Fatalf("clean copy was replaced: %q", text)
	}
}

// Garbled first, clean 40 seconds later from another site: the RFS header
// (page time plus unit code) identifies it as the same page beyond the
// 30-second prefix window, and the clean copy wins.
func TestUpsertRepairsGarbledCopyFromLaterResend(t *testing.T) {
	d, sysA := managedSystemDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	garbled := newPage(sysA, garbledPage, now, &rxproto.PageQuality{Codewords: 40, Corrected: 37, Uncorrectable: 12})
	if out, err := d.UpsertMessageDeduped(ctx, garbled); err != nil || out != MessageCreated {
		t.Fatalf("garbled: %s %v", out, err)
	}
	clean := newPage(sysA, fullPage, now.Add(40*time.Second), &rxproto.PageQuality{Codewords: 40})
	if out, err := d.UpsertMessageDeduped(ctx, clean); err != nil || out != MessageReplaced {
		t.Fatalf("clean: %s %v", out, err)
	}
	if text, _, _ := storedMessage(t, d, garbled.ID); text != fullPage {
		t.Fatalf("stored = %q", text)
	}
}

// Copies from receivers without a quality record are judged by their text:
// a visibly garbled stored copy gives way to a clean one.
func TestUpsertWithoutQualityUsesTextHeuristic(t *testing.T) {
	d, sysA := managedSystemDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	garbled := newPage(sysA, garbledPage, now, nil)
	if out, err := d.UpsertMessageDeduped(ctx, garbled); err != nil || out != MessageCreated {
		t.Fatalf("garbled: %s %v", out, err)
	}
	clean := newPage(sysA, fullPage, now.Add(3*time.Second), nil)
	if out, err := d.UpsertMessageDeduped(ctx, clean); err != nil || out != MessageReplaced {
		t.Fatalf("clean: %s %v", out, err)
	}
}

// Different pages to the same capcode are never merged.
func TestUpsertKeepsDistinctPages(t *testing.T) {
	d, sysA := managedSystemDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	a := newPage(sysA, fullPage, now, &rxproto.PageQuality{Codewords: 40})
	b := newPage(sysA, "13 October 2026 22:49:10 LHPOKOL1 - 26-131321 - Grass fire - FIRECALL - 5 MAIN RD,ABERDARE,CESSNOCK (NSW),2325", now.Add(90*time.Second), &rxproto.PageQuality{Codewords: 30})
	b.IncidentKey = "rfs:26-131321:y"
	for _, m := range []*models.Message{a, b} {
		if out, err := d.UpsertMessageDeduped(ctx, m); err != nil || out != MessageCreated {
			t.Fatalf("%q: %s %v", m.Message[:40], out, err)
		}
	}
	if a.ID == b.ID {
		t.Fatal("distinct pages collapsed into one row")
	}
}
