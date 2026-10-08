package radio

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/blobstore"
	"github.com/the-it-dept/rfspager/internal/p25"
)

// --- WAV ---

func TestEncodeWAVHeader(t *testing.T) {
	pcm := []byte{1, 0, 2, 0, 3, 0, 0xff, 0x7f}
	wav := EncodeWAV(pcm, 8000)
	if len(wav) != 44+len(pcm) {
		t.Fatalf("len = %d", len(wav))
	}
	le := binary.LittleEndian
	checks := []struct {
		name      string
		got, want any
	}{
		{"RIFF", string(wav[0:4]), "RIFF"},
		{"riff size", le.Uint32(wav[4:]), uint32(36 + len(pcm))},
		{"WAVE", string(wav[8:12]), "WAVE"},
		{"fmt", string(wav[12:16]), "fmt "},
		{"fmt size", le.Uint32(wav[16:]), uint32(16)},
		{"format", le.Uint16(wav[20:]), uint16(1)},
		{"channels", le.Uint16(wav[22:]), uint16(1)},
		{"rate", le.Uint32(wav[24:]), uint32(8000)},
		{"byte rate", le.Uint32(wav[28:]), uint32(16000)},
		{"block align", le.Uint16(wav[32:]), uint16(2)},
		{"bits", le.Uint16(wav[34:]), uint16(16)},
		{"data", string(wav[36:40]), "data"},
		{"data size", le.Uint32(wav[40:]), uint32(len(pcm))},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if !bytes.Equal(wav[44:], pcm) {
		t.Error("samples not copied after the header")
	}
	if _, err := (WAVEncoder{}).Encode([]byte{1, 2, 3}, 8000); err == nil {
		t.Error("odd-length PCM accepted")
	}
	if ms := pcmDurationMS(16000, 8000); ms != 1000 {
		t.Errorf("1 s of PCM = %d ms", ms)
	}
}

// --- Assembler ---

var t0 = time.Date(2026, 10, 8, 1, 0, 0, 0, time.UTC)

func chunk(b byte, n int) []byte { return bytes.Repeat([]byte{b}, n) }

func TestAssemblerConcatenatesInOrderAndDropsDuplicates(t *testing.T) {
	a := NewAssembler(AssemblerConfig{})
	meta := CallMeta{Known: true, ID: 7, TGID: 10047}
	if !a.Begin("c1", meta, t0) {
		t.Fatal("Begin refused")
	}
	if r := a.Append("c1", 0, chunk(1, 4), t0); !r.Accepted || r.New || r.Meta != meta {
		t.Fatalf("first chunk: %+v", r)
	}
	a.Append("c1", 1, chunk(2, 4), t0)
	if r := a.Append("c1", 1, chunk(9, 4), t0); r.Accepted {
		t.Error("duplicate seq accepted")
	}
	if r := a.Append("c1", 0, chunk(9, 4), t0); r.Accepted {
		t.Error("old seq accepted")
	}
	a.Append("c1", 2, chunk(3, 4), t0)
	clip := a.Finish("c1", t0.Add(time.Second))
	if clip == nil {
		t.Fatal("no clip")
	}
	if want := append(append(chunk(1, 4), chunk(2, 4)...), chunk(3, 4)...); !bytes.Equal(clip.PCM, want) {
		t.Errorf("PCM = %v, want %v", clip.PCM, want)
	}
	if clip.Chunks != 3 || clip.Missing != 0 || clip.Meta != meta || !clip.StartedAt.Equal(t0) {
		t.Errorf("clip = %+v", clip)
	}
	if calls, bytes := a.Stats(); calls != 0 || bytes != 0 {
		t.Errorf("after finish: %d calls, %d bytes buffered", calls, bytes)
	}
	// The call is over: stray chunks don't start another clip.
	if r := a.Append("c1", 3, chunk(4, 4), t0); r.Accepted {
		t.Error("chunk after the end accepted")
	}
	if a.Finish("c1", t0) != nil {
		t.Error("second Finish returned a clip")
	}
}

func TestAssemblerFillsGapsWithBoundedSilence(t *testing.T) {
	a := NewAssembler(AssemblerConfig{MaxGapFill: 10})
	a.Append("c", 5, chunk(1, 4), t0) // joined mid-call: no leading silence
	a.Append("c", 7, chunk(2, 4), t0) // one chunk lost: 4 bytes of silence
	a.Append("c", 100, chunk(3, 4), t0)
	clip := a.Finish("c", t0)
	want := chunk(1, 4)
	want = append(want, chunk(0, 4)...)
	want = append(want, chunk(2, 4)...)
	want = append(want, chunk(0, 10)...) // 92 lost chunks, capped
	want = append(want, chunk(3, 4)...)
	if !bytes.Equal(clip.PCM, want) {
		t.Errorf("PCM = %v\nwant %v", clip.PCM, want)
	}
	if clip.Missing != 93 || clip.Chunks != 3 {
		t.Errorf("missing %d chunks %d, want 93/3", clip.Missing, clip.Chunks)
	}
}

func TestAssemblerCaps(t *testing.T) {
	t.Run("per call", func(t *testing.T) {
		a := NewAssembler(AssemblerConfig{MaxCallBytes: 10})
		a.Append("c", 0, chunk(1, 6), t0)
		if r := a.Append("c", 1, chunk(2, 6), t0); !r.Accepted {
			t.Error("over-cap chunk not accepted for live forwarding")
		}
		a.Append("c", 2, chunk(3, 6), t0)
		clip := a.Finish("c", t0)
		if len(clip.PCM) != 10 || !clip.Truncated {
			t.Errorf("len %d truncated %v, want 10/true", len(clip.PCM), clip.Truncated)
		}
	})
	t.Run("total", func(t *testing.T) {
		a := NewAssembler(AssemblerConfig{MaxTotalBytes: 12})
		a.Append("a", 0, chunk(1, 8), t0)
		a.Append("b", 0, chunk(2, 8), t0)
		if _, n := a.Stats(); n != 12 {
			t.Errorf("buffered %d, want the 12-byte budget", n)
		}
		if b := a.Finish("b", t0); len(b.PCM) != 4 || !b.Truncated {
			t.Errorf("b = %d bytes truncated %v", len(b.PCM), b.Truncated)
		}
		// Freed budget is usable again.
		a.Append("a", 1, chunk(3, 4), t0)
		if c := a.Finish("a", t0); len(c.PCM) != 12 || c.Truncated {
			t.Errorf("a = %d bytes truncated %v", len(c.PCM), c.Truncated)
		}
	})
	t.Run("calls", func(t *testing.T) {
		a := NewAssembler(AssemblerConfig{MaxCalls: 2})
		a.Begin("a", CallMeta{}, t0)
		a.Append("b", 0, chunk(1, 2), t0)
		if a.Begin("c", CallMeta{}, t0) {
			t.Error("Begin past MaxCalls")
		}
		if r := a.Append("d", 0, chunk(1, 2), t0); r.Accepted {
			t.Error("Append past MaxCalls")
		}
	})
}

func TestAssemblerSweepEvictsAbandonedCalls(t *testing.T) {
	a := NewAssembler(AssemblerConfig{IdleTimeout: 30 * time.Second, MaxAge: time.Minute})
	a.Begin("silent", CallMeta{Known: true, ID: 1}, t0) // encrypted-like: no audio
	a.Append("idle", 0, chunk(1, 4), t0)
	// "chatty" keeps sending, but past MaxAge it is flushed anyway.
	for i := 0; i <= 70; i += 10 {
		a.Append("chatty", i/10, chunk(2, 2), t0.Add(time.Duration(i)*time.Second))
	}
	if got := a.Sweep(t0.Add(20 * time.Second)); len(got) != 0 {
		t.Fatalf("swept %d calls before any timeout", len(got))
	}
	got := a.Sweep(t0.Add(70 * time.Second))
	uids := map[string]bool{}
	for _, c := range got {
		if !c.Evicted {
			t.Errorf("%s not marked evicted", c.UID)
		}
		uids[c.UID] = true
	}
	if len(got) != 2 || !uids["idle"] || !uids["chatty"] {
		t.Errorf("swept %v, want idle and chatty (silent has no audio)", uids)
	}
	if calls, bytes := a.Stats(); calls != 0 || bytes != 0 {
		t.Errorf("after sweep: %d calls, %d bytes", calls, bytes)
	}
	if r := a.Append("idle", 1, chunk(1, 2), t0.Add(71*time.Second)); r.Accepted {
		t.Error("chunk for an evicted call started a new clip")
	}
	// Eventually the evicted ids are forgotten.
	a.Sweep(t0.Add(70*time.Second + doneTTL + time.Second))
	if r := a.Append("idle", 2, chunk(1, 2), t0); !r.Accepted || !r.New {
		t.Error("done set never pruned")
	}
}

// --- Live ---

func recv(t *testing.T, s *LiveSub) (LiveEvent, bool) {
	t.Helper()
	select {
	case ev := <-s.Events():
		return ev, true
	case <-time.After(50 * time.Millisecond):
		return LiveEvent{}, false
	}
}

func TestLiveBrokerFiltersByTalkgroup(t *testing.T) {
	b := NewLiveBroker()
	all, unsubAll := b.Subscribe(nil)
	defer unsubAll()
	one, unsubOne := b.Subscribe([]int{10047})
	defer unsubOne()

	b.PublishAudio(CallMeta{Known: true, ID: 1, TGID: 10047}, "u1", 0, []byte{1, 2})
	b.PublishAudio(CallMeta{Known: true, ID: 2, TGID: 10099}, "u2", 0, []byte{3, 4})
	b.PublishAudio(CallMeta{Known: true, ID: 3}, "u3", 0, []byte{5, 6})                            // unit-to-unit
	b.PublishAudio(CallMeta{Known: true, ID: 4, TGID: 10047, Hidden: true}, "u4", 0, []byte{7, 8}) // hidden
	b.PublishAudio(CallMeta{}, "u5", 0, []byte{9, 9})                                              // row unknown
	b.PublishEnd(CallMeta{Known: true, ID: 1, TGID: 10047}, "u1")

	var gotAll []string
	for {
		ev, ok := recv(t, all)
		if !ok {
			break
		}
		gotAll = append(gotAll, ev.Event+":"+string(ev.Data))
	}
	want := []string{
		`audio:{"call_id":"u1","call":1,"tgid":10047,"seq":0,"pcm":"AQI="}`,
		`audio:{"call_id":"u2","call":2,"tgid":10099,"seq":0,"pcm":"AwQ="}`,
		`audio:{"call_id":"u3","call":3,"tgid":null,"seq":0,"pcm":"BQY="}`,
		`end:{"call_id":"u1","call":1,"tgid":10047}`,
	}
	if strings.Join(gotAll, "\n") != strings.Join(want, "\n") {
		t.Errorf("unfiltered got\n%s\nwant\n%s", strings.Join(gotAll, "\n"), strings.Join(want, "\n"))
	}

	var gotOne []LiveAudio
	for {
		ev, ok := recv(t, one)
		if !ok {
			break
		}
		if ev.Event == "audio" {
			var a LiveAudio
			if err := json.Unmarshal(ev.Data, &a); err != nil {
				t.Fatal(err)
			}
			gotOne = append(gotOne, a)
		}
	}
	if len(gotOne) != 1 || gotOne[0].CallID != 1 || !bytes.Equal(gotOne[0].PCM, []byte{1, 2}) {
		t.Errorf("tg=10047 got %+v, want only call 1", gotOne)
	}
}

func TestLiveBrokerDropsSlowListener(t *testing.T) {
	b := NewLiveBroker()
	slow, unsubSlow := b.Subscribe(nil)
	defer unsubSlow()
	fast, unsubFast := b.Subscribe(nil)
	defer unsubFast()
	meta := CallMeta{Known: true, ID: 1, TGID: 10047}
	done := make(chan struct{})
	received := 0
	go func() {
		defer close(done)
		for range fast.Events() {
			received++
			if received == liveBuffer+10 {
				return
			}
		}
	}()
	for i := 0; i < liveBuffer+10; i++ {
		b.PublishAudio(meta, "u", i, []byte{0, 0}) // never blocks
	}
	select {
	case <-slow.Gone():
	default:
		t.Fatal("listener that never reads was not dropped")
	}
	<-done
	if received != liveBuffer+10 {
		t.Errorf("reading listener got %d of %d chunks", received, liveBuffer+10)
	}
}

// --- Pipeline ---

type fakeRecorder struct {
	mu          sync.Mutex
	ids         map[string]int64
	audio       map[int64]StoredAudio
	transcripts map[int64]string
	errs        map[int64]string
	done        chan struct{}
}

func newFakeRecorder() *fakeRecorder {
	return &fakeRecorder{ids: map[string]int64{}, audio: map[int64]StoredAudio{}, transcripts: map[int64]string{},
		errs: map[int64]string{}, done: make(chan struct{}, 16)}
}

func (f *fakeRecorder) CallIDByUID(_ context.Context, uid string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.ids[uid]
	if !ok {
		return 0, sql.ErrNoRows
	}
	return id, nil
}

func (f *fakeRecorder) SetCallAudio(_ context.Context, id int64, a StoredAudio) error {
	f.mu.Lock()
	f.audio[id] = a
	f.mu.Unlock()
	f.done <- struct{}{}
	return nil
}

func (f *fakeRecorder) SetTranscript(_ context.Context, id int64, text string) error {
	f.mu.Lock()
	f.transcripts[id] = text
	f.mu.Unlock()
	f.done <- struct{}{}
	return nil
}

func (f *fakeRecorder) SetTranscriptError(_ context.Context, id int64, msg string) error {
	f.mu.Lock()
	f.errs[id] = msg
	f.mu.Unlock()
	f.done <- struct{}{}
	return nil
}

func (f *fakeRecorder) wait(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-f.done:
		case <-time.After(2 * time.Second):
			t.Fatalf("waited for %d pipeline writes, got %d", n, i)
		}
	}
}

type fakeASR struct {
	mu      sync.Mutex
	files   []string
	prompts []string
	err     error
}

func (f *fakeASR) Transcribe(_ context.Context, filename string, audio []byte, prompt string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files = append(f.files, filename)
	f.prompts = append(f.prompts, prompt)
	if !bytes.HasPrefix(audio, []byte("RIFF")) {
		return "", errors.New("not a WAV")
	}
	return "Gosford two five responding", f.err
}

func TestPipelineStoresAndTranscribes(t *testing.T) {
	dir := t.TempDir()
	blobs, err := blobstore.NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	rec := newFakeRecorder()
	rec.ids["orphan"] = 99
	asr := &fakeASR{}
	p := newAudioPipeline(rec, PipelineConfig{Blobs: blobs, ASR: asr})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	second := chunk(0, p25.AudioSampleRate*2)
	p.Enqueue(&Clip{UID: "c1", Meta: CallMeta{Known: true, ID: 5}, PCM: second, StartedAt: t0})
	rec.wait(t, 2) // audio, then transcript
	a := rec.audio[5]
	if a.Key != "radio/2026/10/08/5.wav" || a.URL != "/api/v2/mobile/radio/calls/5/audio" ||
		a.ContentType != "audio/wav" || a.MS != 1000 || a.Bytes != 44+len(second) {
		t.Errorf("stored audio = %+v", a)
	}
	obj, err := blobs.Open(ctx, a.Key)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(obj)
	obj.Close()
	if !bytes.Equal(data, EncodeWAV(second, 8000)) {
		t.Error("stored file is not the clip's WAV")
	}
	if rec.transcripts[5] != "Gosford two five responding" {
		t.Errorf("transcript = %q", rec.transcripts[5])
	}

	// A clip never tied to a row is looked up by call_uid; a short one
	// isn't transcribed.
	p.Enqueue(&Clip{UID: "orphan", PCM: chunk(0, 1600), StartedAt: t0}) // 100ms
	rec.wait(t, 1)
	if rec.audio[99].MS != 100 {
		t.Errorf("orphan audio = %+v", rec.audio[99])
	}
	time.Sleep(50 * time.Millisecond)
	if _, ok := rec.transcripts[99]; ok || len(asr.files) != 1 {
		t.Errorf("short clip was transcribed (%v)", asr.files)
	}

	// Transcription failures are recorded against the call.
	asr.err = errors.New("asr down")
	p.Enqueue(&Clip{UID: "c3", Meta: CallMeta{Known: true, ID: 6}, PCM: second, StartedAt: t0})
	rec.wait(t, 2)
	if rec.errs[6] != "asr down" {
		t.Errorf("transcript error = %q", rec.errs[6])
	}
}

func TestPipelineWithoutASRStoresOnly(t *testing.T) {
	blobs, _ := blobstore.NewLocal(t.TempDir())
	rec := newFakeRecorder()
	p := newAudioPipeline(rec, PipelineConfig{Blobs: blobs})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)
	p.Enqueue(&Clip{UID: "c", Meta: CallMeta{Known: true, ID: 1}, PCM: chunk(0, 32000), StartedAt: t0})
	rec.wait(t, 1)
	time.Sleep(20 * time.Millisecond)
	if len(rec.transcripts)+len(rec.errs) != 0 {
		t.Error("transcription attempted with ASR off")
	}
}

func TestPipelineQueueFullDropsWithoutBlocking(t *testing.T) {
	p := newAudioPipeline(newFakeRecorder(), PipelineConfig{QueueSize: 1})
	if !p.Enqueue(&Clip{UID: "a"}) || p.Enqueue(&Clip{UID: "b"}) {
		t.Error("want the first clip queued and the second dropped")
	}
}

// --- Ingest (audio path; the call frames need a database) ---

func TestIngestForwardsAudioOfKnownCallsLive(t *testing.T) {
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	store := NewStore(sqlx.NewDb(conn, "sqlmock"))
	live := NewLiveBroker()
	sub, unsub := live.Subscribe(nil)
	defer unsub()
	in := NewIngest(store, NewAssembler(AssemblerConfig{}), live, nil)
	ctx := context.Background()

	frame := func(uid string, seq int, pcm []byte) json.RawMessage {
		data, _ := json.Marshal(p25.AudioPayload{CallID: uid, Seq: seq, PCM: pcm})
		return data
	}
	// First chunk of a call we never saw start: one lookup ties it to its row.
	mock.ExpectQuery(`SELECT c.id, COALESCE\(c.tgid, 0\)`).WithArgs("known").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tgid", "hidden"}).AddRow(42, 10047, false))
	for seq := 0; seq < 2; seq++ {
		if err := in.HandleFrame(ctx, nil, p25.FrameAudio, frame("known", seq, []byte{1, 0})); err != nil {
			t.Fatal(err)
		}
	}
	// One whose row doesn't exist yet: buffered, but not sent live.
	mock.ExpectQuery(`SELECT c.id`).WithArgs("unknown").WillReturnError(sql.ErrNoRows)
	if err := in.HandleFrame(ctx, nil, p25.FrameAudio, frame("unknown", 0, []byte{2, 0})); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	for seq := 0; seq < 2; seq++ {
		ev, ok := recv(t, sub)
		var a LiveAudio
		if !ok || json.Unmarshal(ev.Data, &a) != nil || a.CallID != 42 || a.Seq != seq || *a.TGID != 10047 {
			t.Fatalf("live event %d = %s (%v)", seq, ev.Data, ok)
		}
	}
	if ev, ok := recv(t, sub); ok {
		t.Errorf("audio of an unknown call went out live: %s", ev.Data)
	}
	if calls, n := in.asm.Stats(); calls != 2 || n != 6 {
		t.Errorf("buffered %d calls / %d bytes, want 2 / 6", calls, n)
	}

	for _, bad := range []string{
		`{"call_id":"","seq":0,"pcm":"AAA="}`,
		`{"call_id":"x","seq":-1,"pcm":"AAA="}`,
		`{"call_id":"x","seq":0,"pcm":"AAAA"}`, // 3 bytes: not whole samples
		`{"call_id":"x","seq":0,"pcm":"not base64!"}`,
	} {
		if err := in.HandleFrame(ctx, nil, p25.FrameAudio, json.RawMessage(bad)); !errors.Is(err, ErrInvalidPayload) {
			t.Errorf("%s: err = %v, want ErrInvalidPayload", bad, err)
		}
	}
}

func TestPipelineGatesBrokenAudioAndPromptsWithTalkgroup(t *testing.T) {
	blobs, _ := blobstore.NewLocal(t.TempDir())
	rec := newFakeRecorder()
	asr := &fakeASR{}
	p := newAudioPipeline(rec, PipelineConfig{Blobs: blobs, ASR: asr})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)
	twoSec := chunk(0, p25.AudioSampleRate*4)

	// 30% of frames uncorrectable: stored, but not sent to the ASR.
	p.Enqueue(&Clip{UID: "bad", Meta: CallMeta{Known: true, ID: 7}, PCM: twoSec, StartedAt: t0, Frames: 100, BadFrames: 30})
	rec.wait(t, 2) // audio, then the poor_audio marker
	if rec.errs[7] != PoorAudio {
		t.Errorf("broken clip: transcript_error = %q", rec.errs[7])
	}

	// A clean one is transcribed with its talkgroup in the prompt.
	meta := CallMeta{Known: true, ID: 8, AlphaTag: "RFS CENCO A", Agency: "RFS"}
	p.Enqueue(&Clip{UID: "good", Meta: meta, PCM: twoSec, StartedAt: t0, Frames: 100, BadFrames: 2})
	rec.wait(t, 2)
	if len(asr.files) != 1 || asr.prompts[0] != "Talkgroup: RFS CENCO A (RFS)." {
		t.Errorf("asr calls %v prompts %q", asr.files, asr.prompts)
	}
}
