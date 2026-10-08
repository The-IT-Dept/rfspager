package radio

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/the-it-dept/rfspager/internal/blobstore"
	"github.com/the-it-dept/rfspager/internal/p25"
)

// radioTestDB resets the test database to just what migrations 032 and 033
// need (a systems table) and applies both twice, which also proves them
// idempotent.
func radioTestDB(t *testing.T) (*sqlx.DB, string) {
	t.Helper()
	raw := os.Getenv("RFSPAGER_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("RFSPAGER_TEST_DATABASE_URL is not set")
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.HasPrefix(strings.TrimPrefix(u.Path, "/"), "rfspager_test") {
		t.Fatalf("refusing to reset non-test database %q", raw)
	}
	db, err := sqlx.Connect("postgres", raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
		DROP SCHEMA public CASCADE;
		CREATE SCHEMA public;
		CREATE TABLE systems (id BIGSERIAL PRIMARY KEY);
		INSERT INTO systems DEFAULT VALUES;
	`); err != nil {
		t.Fatalf("schema: %v", err)
	}
	for i := 0; i < 2; i++ {
		for _, name := range []string{"032_radio.sql", "033_radio_audio.sql"} {
			migration, err := os.ReadFile("../db/migrations/" + name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(string(migration)); err != nil {
				t.Fatalf("apply %s (pass %d): %v", name, i+1, err)
			}
		}
	}
	return db, raw
}

const testReceiver = 1 // the systems row radioTestDB inserts

func testCall(id string, start time.Time) p25.CallPayload {
	return p25.CallPayload{
		CallID: id, SysID: DefaultSysID, RFSS: 1, Site: 51, Group: 10047,
		FreqHz: 413_512_500, StartMS: start.UnixMilli(),
	}
}

type tgCounts struct {
	Clear     int        `db:"calls_clear"`
	Encrypted int        `db:"calls_encrypted"`
	LastSeen  *time.Time `db:"last_seen_at"`
}

func talkgroupCounts(t *testing.T, db *sqlx.DB, tgid int) tgCounts {
	t.Helper()
	var c tgCounts
	if err := db.Get(&c, `SELECT calls_clear, calls_encrypted, last_seen_at FROM radio_talkgroups WHERE sysid = $1 AND tgid = $2`, DefaultSysID, tgid); err != nil {
		t.Fatalf("talkgroup %d: %v", tgid, err)
	}
	return c
}

func TestStartAndEndCallAreIdempotentAndCountOnce(t *testing.T) {
	db, _ := radioTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	start := time.Now().Add(-20 * time.Second).Truncate(time.Millisecond)

	p := testCall("call-1", start)
	for i, want := range []bool{true, false} {
		got, err := s.StartCall(ctx, testReceiver, p)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("StartCall #%d = %v, want %v", i+1, got, want)
		}
	}
	if c := talkgroupCounts(t, db, 10047); c.Clear != 0 || c.Encrypted != 0 || c.LastSeen == nil || !c.LastSeen.Equal(start) {
		t.Errorf("after start: %+v, want no counts and last_seen %v", c, start)
	}

	// The end knows the source and that the call was encrypted; the start
	// didn't.
	p.Source, p.Encrypted = 2039407, true
	p.EndMS = start.Add(8 * time.Second).UnixMilli()
	for i, want := range []bool{true, false, false} {
		got, err := s.EndCall(ctx, testReceiver, p)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("EndCall #%d = %v, want %v", i+1, got, want)
		}
	}
	if c := talkgroupCounts(t, db, 10047); c.Clear != 0 || c.Encrypted != 1 {
		t.Errorf("after 3 call_ends: clear=%d encrypted=%d, want 0/1", c.Clear, c.Encrypted)
	}

	var row struct {
		N         int        `db:"n"`
		Source    *int       `db:"source_rid"`
		Encrypted bool       `db:"encrypted"`
		EndedAt   *time.Time `db:"ended_at"`
	}
	if err := db.Get(&row, `SELECT COUNT(*) OVER () AS n, source_rid, encrypted, ended_at FROM radio_calls`); err != nil {
		t.Fatal(err)
	}
	if row.N != 1 || row.Source == nil || *row.Source != 2039407 || !row.Encrypted || row.EndedAt == nil ||
		!row.EndedAt.Equal(start.Add(8*time.Second)) {
		t.Errorf("call row = %+v", row)
	}
	var units int
	if err := db.Get(&units, `SELECT COUNT(*) FROM radio_units WHERE rid = 2039407`); err != nil || units != 1 {
		t.Errorf("source unit rows = %d (%v), want 1", units, err)
	}
}

func TestEndCallWithoutStartInsertsAndCounts(t *testing.T) {
	db, _ := radioTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	start := time.Now().Add(-time.Minute)

	p := testCall("orphan-end", start)
	p.EndMS = start.Add(5 * time.Second).UnixMilli()
	if ended, err := s.EndCall(ctx, testReceiver, p); err != nil || !ended {
		t.Fatalf("EndCall = %v, %v", ended, err)
	}
	// A late call_start for an already-ended call must not reopen it.
	if started, err := s.StartCall(ctx, testReceiver, testCall("orphan-end", start)); err != nil || started {
		t.Fatalf("late StartCall = %v, %v; want a no-op", started, err)
	}
	var open int
	if err := db.Get(&open, `SELECT COUNT(*) FROM radio_calls WHERE ended_at IS NULL`); err != nil || open != 0 {
		t.Errorf("open calls = %d (%v), want 0", open, err)
	}
	if c := talkgroupCounts(t, db, 10047); c.Clear != 1 {
		t.Errorf("calls_clear = %d, want 1", c.Clear)
	}
}

func TestConcurrentEndCallCountsExactlyOnce(t *testing.T) {
	db, _ := radioTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	start := time.Now().Add(-time.Minute)

	// Half the racers find the call started, half race to insert it too.
	for _, started := range []bool{true, false} {
		id := "race-open"
		if !started {
			id = "race-missing"
		} else if _, err := s.StartCall(ctx, testReceiver, testCall(id, start)); err != nil {
			t.Fatal(err)
		}
		p := testCall(id, start)
		p.EndMS = start.Add(3 * time.Second).UnixMilli()

		const racers = 8
		var (
			wg    sync.WaitGroup
			mu    sync.Mutex
			ended int
		)
		gate := make(chan struct{})
		for i := 0; i < racers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-gate
				ok, err := s.EndCall(ctx, testReceiver, p)
				if err != nil {
					t.Errorf("EndCall: %v", err)
					return
				}
				if ok {
					mu.Lock()
					ended++
					mu.Unlock()
				}
			}()
		}
		close(gate)
		wg.Wait()
		if ended != 1 {
			t.Errorf("%s: %d of %d concurrent call_ends reported the transition, want 1", id, ended, racers)
		}
	}
	if c := talkgroupCounts(t, db, 10047); c.Clear != 2 {
		t.Errorf("calls_clear = %d, want 2 (one per call)", c.Clear)
	}
}

func TestRecordUnitTracksAcceptedAffiliations(t *testing.T) {
	db, _ := radioTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	at := time.Now().Add(-time.Minute)

	events := []p25.UnitPayload{
		{Kind: p25.UnitKindRegistration, SysID: DefaultSysID, RFSS: 1, Site: 51, Unit: 2039407, TSMS: at.UnixMilli()},
		{Kind: p25.UnitKindAffiliation, SysID: DefaultSysID, RFSS: 1, Site: 51, Unit: 2039407, Group: 10047, TSMS: at.Add(time.Second).UnixMilli()},
		// Refused: must not move the unit off 10047.
		{Kind: p25.UnitKindAffiliation, SysID: DefaultSysID, RFSS: 1, Site: 51, Unit: 2039407, Group: 10099, Result: 1, TSMS: at.Add(2 * time.Second).UnixMilli()},
	}
	for _, e := range events {
		if err := s.RecordUnit(ctx, testReceiver, e); err != nil {
			t.Fatal(err)
		}
	}
	var u struct {
		LastTGID *int      `db:"last_tgid"`
		LastSeen time.Time `db:"last_seen_at"`
	}
	if err := db.Get(&u, `SELECT last_tgid, last_seen_at FROM radio_units WHERE sysid = $1 AND rid = 2039407`, DefaultSysID); err != nil {
		t.Fatal(err)
	}
	if u.LastTGID == nil || *u.LastTGID != 10047 {
		t.Errorf("last_tgid = %v, want 10047", u.LastTGID)
	}
	if want := at.Add(2 * time.Second).Truncate(time.Millisecond); !u.LastSeen.Equal(want) {
		t.Errorf("last_seen_at = %v, want %v", u.LastSeen, want)
	}
	var n int
	if err := db.Get(&n, `SELECT COUNT(*) FROM radio_unit_events`); err != nil || n != 3 {
		t.Errorf("unit events = %d (%v), want 3", n, err)
	}
	if c := talkgroupCounts(t, db, 10047); c.LastSeen == nil {
		t.Error("accepted affiliation did not mark its talkgroup seen")
	}
	var refused int
	if err := db.Get(&refused, `SELECT COUNT(*) FROM radio_talkgroups WHERE tgid = 10099`); err != nil || refused != 0 {
		t.Errorf("refused affiliation created talkgroup rows: %d (%v)", refused, err)
	}
}

func TestUpsertSiteKeepsKnownNACAndAdminFields(t *testing.T) {
	db, _ := radioTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	site := p25.SitePayload{
		WACN: 0xBEE00, SysID: DefaultSysID, RFSS: 1, Site: 51, NAC: 0x2D1, ControlHz: 413_250_000,
		Neighbors: []p25.NeighborSite{{SysID: DefaultSysID, RFSS: 1, Site: 52, ControlHz: 413_300_000}},
	}
	if err := s.UpsertSite(ctx, testReceiver, site, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE radio_sites SET name = 'Mt Elliot', lat = -33.4 WHERE site = 51`); err != nil {
		t.Fatal(err)
	}
	site.NAC, site.Neighbors = 0, nil
	if err := s.UpsertSite(ctx, testReceiver, site, time.Now()); err != nil {
		t.Fatal(err)
	}
	sites, err := s.ListSites(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 {
		t.Fatalf("sites = %+v", sites)
	}
	got := sites[0]
	if got.NAC != 0x2D1 || got.WACN != 0xBEE00 || got.Name == nil || *got.Name != "Mt Elliot" || !got.Active ||
		string(got.Neighbors) != "[]" {
		t.Errorf("site = %+v (neighbors %s)", got, got.Neighbors)
	}
}

func TestListCallsPagesNewestFirstAndHidesHiddenTalkgroups(t *testing.T) {
	db, _ := radioTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		p := testCall("c"+string(rune('a'+i)), base.Add(time.Duration(i)*time.Minute))
		if i == 4 {
			p.Group = 20000 // to be hidden
		}
		if _, err := s.StartCall(ctx, testReceiver, p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE radio_talkgroups SET hidden = true, alpha_tag = 'Secret' WHERE tgid = 20000`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE radio_talkgroups SET alpha_tag = 'RFS Central Coast A' WHERE tgid = 10047`); err != nil {
		t.Fatal(err)
	}

	page1, more, err := s.ListCalls(ctx, CallFilter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 2 || !more || page1[0].TGAlphaTag == nil || *page1[0].TGAlphaTag != "RFS Central Coast A" {
		t.Fatalf("page 1 = %+v more=%v", page1, more)
	}
	if !page1[0].StartedAt.After(page1[1].StartedAt) {
		t.Error("page 1 not newest first")
	}
	page2, more, err := s.ListCalls(ctx, CallFilter{Limit: 2, BeforeID: page1[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 2 || more || !page2[0].StartedAt.Before(page1[1].StartedAt) {
		t.Fatalf("page 2 = %+v more=%v", page2, more)
	}
	byTG, _, err := s.ListCalls(ctx, CallFilter{TGID: 20000, Limit: 10})
	if err != nil || len(byTG) != 0 {
		t.Errorf("hidden talkgroup calls = %d (%v), want 0", len(byTG), err)
	}
	tgs, err := s.ListTalkgroups(ctx)
	if err != nil || len(tgs) != 1 {
		t.Errorf("talkgroups = %+v (%v), want only the visible one", tgs, err)
	}
}

func TestImportTalkgroupsKeepsCountersAndBlankCells(t *testing.T) {
	db, _ := radioTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	start := time.Now().Add(-time.Minute)
	p := testCall("counted", start)
	p.EndMS = start.Add(time.Second).UnixMilli()
	if _, err := s.EndCall(ctx, testReceiver, p); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE radio_talkgroups SET agency = 'RFS', hidden = true WHERE tgid = 10047`); err != nil {
		t.Fatal(err)
	}

	rows, _, err := ParseTalkgroupCSV(strings.NewReader("tgid,alpha_tag,agency\n10047,RFS Central Coast A,\n10050,RFS CC Ops,RFS\n"))
	if err != nil {
		t.Fatal(err)
	}
	inserted, updated, err := s.ImportTalkgroups(ctx, DefaultSysID, rows)
	if err != nil {
		t.Fatal(err)
	}
	if inserted != 1 || updated != 1 {
		t.Errorf("inserted=%d updated=%d, want 1/1", inserted, updated)
	}
	var tg struct {
		AlphaTag string     `db:"alpha_tag"`
		Agency   string     `db:"agency"`
		Hidden   bool       `db:"hidden"`
		Clear    int        `db:"calls_clear"`
		LastSeen *time.Time `db:"last_seen_at"`
	}
	if err := db.Get(&tg, `SELECT alpha_tag, agency, hidden, calls_clear, last_seen_at FROM radio_talkgroups WHERE tgid = 10047`); err != nil {
		t.Fatal(err)
	}
	if tg.AlphaTag != "RFS Central Coast A" || tg.Agency != "RFS" || !tg.Hidden || tg.Clear != 1 || tg.LastSeen == nil {
		t.Errorf("updated talkgroup = %+v", tg)
	}
	var neverHeard *time.Time
	if err := db.Get(&neverHeard, `SELECT last_seen_at FROM radio_talkgroups WHERE tgid = 10050`); err != nil || neverHeard != nil {
		t.Errorf("imported-only talkgroup last_seen_at = %v (%v), want NULL", neverHeard, err)
	}
}

func TestCallTriggerNotifiesOnEachStageOnce(t *testing.T) {
	db, raw := radioTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	l := pq.NewListener(raw, time.Second, time.Second, nil)
	t.Cleanup(func() { _ = l.Close() })
	if err := l.Listen("radio_call"); err != nil {
		t.Fatal(err)
	}
	next := func() (string, bool) {
		select {
		case n := <-l.Notify:
			if n == nil {
				return "", false
			}
			return n.Extra, true
		case <-time.After(500 * time.Millisecond):
			return "", false
		}
	}

	start := time.Now().Add(-time.Minute)
	p := testCall("notify", start)
	if _, err := s.StartCall(ctx, testReceiver, p); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := db.Get(&id, `SELECT id::text FROM radio_calls WHERE call_uid = 'notify'`); err != nil {
		t.Fatal(err)
	}
	if got, ok := next(); !ok || got != id {
		t.Fatalf("start notify = %q, %v; want %s", got, ok, id)
	}
	p.EndMS = start.Add(time.Second).UnixMilli()
	for i := 0; i < 2; i++ {
		if _, err := s.EndCall(ctx, testReceiver, p); err != nil {
			t.Fatal(err)
		}
	}
	if got, ok := next(); !ok || got != id {
		t.Fatalf("end notify = %q, %v; want %s", got, ok, id)
	}
	if got, ok := next(); ok {
		t.Errorf("duplicate call_end notified again (%s)", got)
	}
	// Audio landing and a transcript each notify once (033).
	idNum, _ := strconv.ParseInt(id, 10, 64)
	audio := StoredAudio{Key: "radio/k.wav", ContentType: "audio/wav", URL: AudioURL(idNum), MS: 1000, Bytes: 16044}
	for i := 0; i < 2; i++ {
		if err := s.SetCallAudio(ctx, idNum, audio); err != nil {
			t.Fatal(err)
		}
	}
	if got, ok := next(); !ok || got != id {
		t.Fatalf("audio notify = %q, %v; want %s", got, ok, id)
	}
	if got, ok := next(); ok {
		t.Errorf("re-storing the same audio notified again (%s)", got)
	}
	if err := s.SetTranscript(ctx, idNum, "x"); err != nil {
		t.Fatal(err)
	}
	if got, ok := next(); !ok || got != id {
		t.Fatalf("transcript notify = %q, %v; want %s", got, ok, id)
	}
	// Edits that touch none of those stay quiet, including transcription
	// failures.
	if err := s.SetTranscriptError(ctx, idNum, "boom"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE radio_calls SET emergency = true`); err != nil {
		t.Fatal(err)
	}
	if got, ok := next(); ok {
		t.Errorf("unrelated update notified (%s)", got)
	}
}

// A call's start, voice and end through Ingest, as a receiver sends them:
// the clip is stored, the row points at the API path and gets a transcript,
// and GetCall serves both. An encrypted call ends with no audio.
func TestIngestRecordsAndTranscribesCallAudio(t *testing.T) {
	db, _ := radioTestDB(t)
	s := NewStore(db)
	blobs, err := blobstore.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	asr := &fakeASR{}
	in := NewIngest(s, NewAssembler(AssemblerConfig{}), NewLiveBroker(),
		NewAudioPipeline(s, PipelineConfig{Blobs: blobs, ASR: asr}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go in.Run(ctx)

	send := func(frameType string, v any) {
		t.Helper()
		data, _ := json.Marshal(v)
		if err := in.HandleFrame(ctx, nil, frameType, data); err != nil {
			t.Fatalf("%s: %v", frameType, err)
		}
	}
	start := time.Now().Add(-10 * time.Second)
	p := testCall("voice-1", start)
	send(p25.FrameCallStart, p)
	for seq := 0; seq < 5; seq++ {
		send(p25.FrameAudio, p25.AudioPayload{CallID: p.CallID, Seq: seq, PCM: make([]byte, 3200)}) // 200ms each
	}
	p.EndMS = start.Add(time.Second).UnixMilli()
	send(p25.FrameCallEnd, p)

	enc := testCall("enc-1", start)
	enc.Encrypted = true
	send(p25.FrameCallStart, enc)
	enc.EndMS = start.Add(time.Second).UnixMilli()
	send(p25.FrameCallEnd, enc)

	id, err := s.CallIDByUID(ctx, "voice-1")
	if err != nil {
		t.Fatal(err)
	}
	var call *Call
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if call, err = s.GetCall(ctx, id); err != nil {
			t.Fatal(err)
		}
		if call.TranscribedAt != nil {
			break
		}
	}
	if call.AudioURL == nil || *call.AudioURL != AudioURL(id) || call.AudioMS == nil || *call.AudioMS != 1000 {
		t.Errorf("audio_url %v audio_ms %v", call.AudioURL, call.AudioMS)
	}
	if call.Transcript == nil || *call.Transcript != "Gosford two five responding" || call.TranscribedAt == nil {
		t.Errorf("transcript %v at %v", call.Transcript, call.TranscribedAt)
	}
	a, err := s.GetCallAudio(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if a.ContentType != "audio/wav" || a.Bytes != 44+16000 {
		t.Errorf("call audio = %+v", a)
	}
	obj, err := blobs.Open(ctx, a.Key)
	if err != nil {
		t.Fatalf("stored object: %v", err)
	}
	obj.Close()

	encID, _ := s.CallIDByUID(ctx, "enc-1")
	if _, err := s.GetCallAudio(ctx, encID); !IsNotFound(err) {
		t.Errorf("encrypted call audio: %v, want not found", err)
	}
	// Hidden talkgroups hide the recording too.
	if _, err := db.Exec(`UPDATE radio_talkgroups SET hidden = true WHERE tgid = 10047`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetCallAudio(ctx, id); !IsNotFound(err) {
		t.Errorf("hidden talkgroup call audio: %v, want not found", err)
	}
}
