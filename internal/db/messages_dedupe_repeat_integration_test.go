package db

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/the-it-dept/rfspager/internal/models"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

func seedCapcode(t *testing.T, d *DB, address, alias string) sql.NullInt64 {
	t.Helper()
	var id int64
	if err := d.conn.QueryRowContext(context.Background(),
		`INSERT INTO capcodes (address, alias, agency, icon, colour, "ignore") VALUES ($1, $2, 'Lower Hunter', 'fire', 'red', false) RETURNING id`,
		address, alias).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return sql.NullInt64{Int64: id, Valid: true}
}

// The 26-131349 sequence of 2026-10-14 07:46 replayed in the order the
// server received it. Before this fix the two garbled third-transmission
// copies and the phantom-capcode copy each became a message, and the first
// two woke all 114 subscribers a second and third time.
func TestUpsertCollapsesDamagedResendsOfTheSamePage(t *testing.T) {
	d, newcastle := managedSystemDB(t)
	budgewoi := seedManagedSystem(t, d, "budgewoi")
	raymondTerrace := seedCapcode(t, d, "0010864", "Raymond Terrace Brigade")
	ctx := context.Background()
	at := time.Date(2026, 10, 13, 20, 46, 10, 0, time.UTC)

	page := func(sys int64, address string, cap sql.NullInt64, text string, after time.Duration, q rxproto.PageQuality) *models.Message {
		return &models.Message{SystemID: sys, Address: address, CapcodeID: cap, Message: text, CreatedAt: at.Add(after), Quality: &q}
	}
	steps := []struct {
		name string
		msg  *models.Message
		want string
	}{
		{"LHRAYTE, Newcastle, clean", page(newcastle, "0010864", raymondTerrace, rayteClean, 0, rxproto.PageQuality{Codewords: 54, Corrected: 9}), MessageCreated},
		{"LHRAYTE7, Newcastle, clean", page(newcastle, "0010864", raymondTerrace, rayte7Clean, 5*time.Second, rxproto.PageQuality{Codewords: 54, Corrected: 8}), MessageCreated},
		{"phantom capcode 1041008, Budgewoi", page(budgewoi, "1041008", sql.NullInt64{}, raytePhantom, 10*time.Second, rxproto.PageQuality{Codewords: 31, Corrected: 20, Uncorrectable: 10, Truncated: true}), MessageDuplicate},
		{"LHRAYTE1, Newcastle, clean: same dispatch second", page(newcastle, "0010864", raymondTerrace, rayte1Clean, 12*time.Second, rxproto.PageQuality{Codewords: 54, Corrected: 8}), MessageDuplicate},
		{"LHRAYTE re-sent, both receivers", page(budgewoi, "0010864", raymondTerrace, rayteClean, 20*time.Second, rxproto.PageQuality{Codewords: 54}), MessageDuplicate},
		{"LHRaYE, Budgewoi, third transmission", page(budgewoi, "0010864", raymondTerrace, rayteGarbled, 40*time.Second, rxproto.PageQuality{Codewords: 54, Corrected: 40}), MessageDuplicate},
		{"LHRAYTE7 third transmission, text intact", page(budgewoi, "0010864", raymondTerrace, rayte7Clean, 44*time.Second, rxproto.PageQuality{Codewords: 54, Corrected: 30}), MessageDuplicate},
		{"26-1149, Budgewoi, third transmission", page(budgewoi, "0010864", raymondTerrace, rayte1Garbled, 52*time.Second, rxproto.PageQuality{Codewords: 54, Corrected: 34, Uncorrectable: 1}), MessageDuplicate},
	}
	for _, s := range steps {
		out, err := d.UpsertMessageDeduped(ctx, s.msg)
		if err != nil || out != s.want {
			t.Fatalf("%s: got %s, want %s (err %v)", s.name, out, s.want, err)
		}
	}
	var stored []string
	if err := d.conn.SelectContext(ctx, &stored, `SELECT message FROM messages WHERE deleted_at IS NULL ORDER BY id`); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 || stored[0] != rayteClean || stored[1] != rayte7Clean {
		t.Fatalf("stored = %q", stored)
	}

	// A genuinely different page to the brigade inside the window is still
	// stored even when the receiver struggled with it, and so is the stop
	// message.
	other := page(budgewoi, "0010864", raymondTerrace, rayteOtherJob, 80*time.Second, rxproto.PageQuality{Codewords: 50, Corrected: 33, Uncorrectable: 1})
	if out, err := d.UpsertMessageDeduped(ctx, other); err != nil || out != MessageCreated {
		t.Fatalf("other job: %s %v", out, err)
	}
	stop := page(newcastle, "0010864", raymondTerrace, rayteStop, 12*time.Minute, rxproto.PageQuality{Codewords: 30, Corrected: 1})
	if out, err := d.UpsertMessageDeduped(ctx, stop); err != nil || out != MessageCreated {
		t.Fatalf("stop: %s %v", out, err)
	}
}

// The damaged copy arriving first used to be stuck: a mis-corrected copy
// with nothing unrepairable tied with the clean one on damage and the tie
// went to the stored text. Corrections now count, so the clean copy
// replaces it and pending pushes are rewritten.
func TestUpsertReplacesMiscorrectedCopyWhenCleanOneFollows(t *testing.T) {
	d, newcastle := managedSystemDB(t)
	budgewoi := seedManagedSystem(t, d, "budgewoi")
	raymondTerrace := seedCapcode(t, d, "0010864", "Raymond Terrace Brigade")
	ctx := context.Background()
	at := time.Date(2026, 10, 13, 20, 46, 10, 0, time.UTC)

	garbled := &models.Message{SystemID: budgewoi, Address: "0010864", CapcodeID: raymondTerrace, Message: rayteGarbled, CreatedAt: at,
		Quality: &rxproto.PageQuality{Codewords: 54, Corrected: 40}}
	if out, err := d.UpsertMessageDeduped(ctx, garbled); err != nil || out != MessageCreated {
		t.Fatalf("garbled first: %s %v", out, err)
	}
	clean := &models.Message{SystemID: newcastle, Address: "0010864", CapcodeID: raymondTerrace, Message: rayteClean, CreatedAt: at.Add(45 * time.Second),
		Quality: &rxproto.PageQuality{Codewords: 54, Corrected: 9}}
	out, err := d.UpsertMessageDeduped(ctx, clean)
	if err != nil || out != MessageReplaced || clean.ID != garbled.ID {
		t.Fatalf("clean: out=%s err=%v id=%d want %d", out, err, clean.ID, garbled.ID)
	}
	if text, replaced, system := storedMessage(t, d, garbled.ID); text != rayteClean || replaced != 1 || system != newcastle {
		t.Fatalf("stored = %q replaced=%d system=%d", text, replaced, system)
	}
}

// A clean page that merely resembles another clean page on the capcode is
// a different page: the similarity rule needs a damaged copy on one side.
func TestUpsertKeepsSimilarCleanPagesApart(t *testing.T) {
	d, newcastle := managedSystemDB(t)
	raymondTerrace := seedCapcode(t, d, "0010864", "Raymond Terrace Brigade")
	ctx := context.Background()
	at := time.Date(2026, 10, 13, 20, 46, 10, 0, time.UTC)

	first := &models.Message{SystemID: newcastle, Address: "0010864", CapcodeID: raymondTerrace, Message: rayteClean, CreatedAt: at,
		Quality: &rxproto.PageQuality{Codewords: 54, Corrected: 2}}
	// Same job re-paged a minute later with a different time and one extra word.
	repage := &models.Message{SystemID: newcastle, Address: "0010864", CapcodeID: raymondTerrace,
		Message:   "14 October 2026 07:47:40 LHRAYTE - 26-131349 - Power pole alight - FIRECALL - 2ND ALARM - 158F MASONITE RD,TOMAGO,PORT STEPHENS (NSW),2322 - [151.763336,-32.801304]",
		CreatedAt: at.Add(97 * time.Second), Quality: &rxproto.PageQuality{Codewords: 58, Corrected: 3}}
	for _, m := range []*models.Message{first, repage} {
		if out, err := d.UpsertMessageDeduped(ctx, m); err != nil || out != MessageCreated {
			t.Fatalf("%q: %s %v", m.Message[:40], out, err)
		}
	}
	if first.ID == repage.ID {
		t.Fatal("two clean pages collapsed into one")
	}
}
