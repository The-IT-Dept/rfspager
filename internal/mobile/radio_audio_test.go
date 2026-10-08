package mobile

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/authjwt"
	"github.com/the-it-dept/rfspager/internal/blobstore"
	"github.com/the-it-dept/rfspager/internal/radio"
)

var userColumns = []string{"id", "user_id", "name", "email", "subscription_valid", "subscription_product_id",
	"subscription_expiry", "will_renew", "subscription_platform", "superuser", "token_version"}

func expectUser(mock sqlmock.Sqlmock, id int64, subscribed bool) {
	mock.ExpectQuery(`SELECT[\s\S]*FROM users WHERE id`).
		WillReturnRows(sqlmock.NewRows(userColumns).AddRow(id, "uuid", nil, "u@example.invalid", subscribed, nil, nil, nil, nil, false, 0))
}

func TestRadioCallAudio(t *testing.T) {
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	db := sqlx.NewDb(conn, "sqlmock")

	blobs, err := blobstore.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pcm := make([]byte, 16000)
	for i := range pcm {
		pcm[i] = byte(i)
	}
	wav := radio.EncodeWAV(pcm, 8000)
	if _, err := blobs.Put(context.Background(), "radio/2026/10/08/7.wav", "audio/wav", bytes.NewReader(wav), int64(len(wav))); err != nil {
		t.Fatal(err)
	}

	router := RadioRouter(db, "test-secret", radio.NewStore(db), nil, nil, blobs)
	get := func(path string, userID int64, header ...string) *httptest.ResponseRecorder {
		tok, err := authjwt.Issue([]byte("test-secret"), "rfspager-mobile", "mobile", userID, 0, time.Hour, nil)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		for i := 0; i+1 < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		return rr
	}
	audioQuery := `SELECT c.audio_key[\s\S]*tg.hidden IS TRUE OR COALESCE\(tg.agency, ''\) NOT IN \('RFS', 'FRNSW'\)`
	audioRow := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"audio_key", "audio_content_type", "audio_bytes"}).
			AddRow("radio/2026/10/08/7.wav", "audio/wav", len(wav))
	}

	// Free users are sent to the paywall before anything is looked up.
	expectUser(mock, 1, false)
	if rr := get("/calls/7/audio", 1); rr.Code != http.StatusPaymentRequired {
		t.Fatalf("free user = %d, want 402", rr.Code)
	}

	// Whole file.
	expectUser(mock, 2, true)
	mock.ExpectQuery(audioQuery).WithArgs(int64(7)).WillReturnRows(audioRow())
	rr := get("/calls/7/audio", 2)
	if rr.Code != http.StatusOK || rr.Header().Get("Content-Type") != "audio/wav" ||
		rr.Header().Get("Accept-Ranges") != "bytes" || rr.Header().Get("Content-Length") != "16044" ||
		!bytes.Equal(rr.Body.Bytes(), wav) {
		t.Fatalf("full GET = %d %v (%d bytes)", rr.Code, rr.Header(), rr.Body.Len())
	}

	// A range, as a browser's <audio> seeking sends.
	expectUser(mock, 2, true)
	mock.ExpectQuery(audioQuery).WithArgs(int64(7)).WillReturnRows(audioRow())
	rr = get("/calls/7/audio", 2, "Range", "bytes=44-143")
	if rr.Code != http.StatusPartialContent || rr.Header().Get("Content-Range") != "bytes 44-143/16044" ||
		rr.Header().Get("Content-Length") != "100" || !bytes.Equal(rr.Body.Bytes(), pcm[:100]) {
		t.Fatalf("ranged GET = %d %v", rr.Code, rr.Header())
	}

	// Hidden talkgroup, no recording or unknown call: the query matches
	// nothing.
	expectUser(mock, 2, true)
	mock.ExpectQuery(audioQuery).WithArgs(int64(8)).WillReturnError(sql.ErrNoRows)
	if rr := get("/calls/8/audio", 2); rr.Code != http.StatusNotFound {
		t.Fatalf("no audio = %d, want 404", rr.Code)
	}

	// Row says there is audio, storage disagrees.
	expectUser(mock, 2, true)
	mock.ExpectQuery(audioQuery).WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"audio_key", "audio_content_type", "audio_bytes"}).AddRow("radio/gone.wav", "audio/wav", 10))
	if rr := get("/calls/9/audio", 2); rr.Code != http.StatusNotFound {
		t.Fatalf("missing blob = %d, want 404", rr.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRadioLiveStreamsFilteredAudio(t *testing.T) {
	live := radio.NewLiveBroker()
	h := &radioHandlers{live: live}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(context.WithValue(r.Context(), UserContextKey, &AuthUser{ID: 2, SubscriptionValid: true}))
		h.Live(w, r)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/live?tg=10047,10048")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("GET /live = %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	lines := bufio.NewReader(resp.Body)
	if l, _ := lines.ReadString('\n'); l != ": connected\n" {
		t.Fatalf("first line %q", l)
	}
	lines.ReadString('\n')

	// The handler subscribes before writing ": connected".
	live.PublishAudio(radio.CallMeta{Known: true, ID: 1, TGID: 10099}, "other", 0, []byte{9, 9})
	live.PublishAudio(radio.CallMeta{Known: true, ID: 2, TGID: 10048}, "wanted", 0, []byte{1, 0})
	live.PublishEnd(radio.CallMeta{Known: true, ID: 2, TGID: 10048}, "wanted")

	read := func() (event, data string) {
		for {
			l, err := lines.ReadString('\n')
			if err != nil {
				t.Fatalf("stream ended: %v", err)
			}
			l = strings.TrimSuffix(l, "\n")
			switch {
			case strings.HasPrefix(l, "event: "):
				event = strings.TrimPrefix(l, "event: ")
			case strings.HasPrefix(l, "data: "):
				data = strings.TrimPrefix(l, "data: ")
			case l == "" && event != "":
				return event, data
			}
		}
	}
	ev, data := read()
	var a radio.LiveAudio
	if ev != "audio" || json.Unmarshal([]byte(data), &a) != nil || a.CallUID != "wanted" || a.CallID != 2 || a.TGID == nil || *a.TGID != 10048 {
		t.Fatalf("first event %s %s, want call 2's audio (10099 filtered out)", ev, data)
	}
	if ev, data := read(); ev != "end" || !strings.Contains(data, `"call":2`) {
		t.Fatalf("second event %s %s, want call 2's end", ev, data)
	}
}

func TestRadioLiveRejectsBadTalkgroupFilter(t *testing.T) {
	h := &radioHandlers{live: radio.NewLiveBroker()}
	for _, q := range []string{"abc", "0", "70000", "1,,2", strings.Repeat("1,", maxLiveTalkgroups) + "1"} {
		rr := httptest.NewRecorder()
		h.Live(rr, httptest.NewRequest(http.MethodGet, "/live?tg="+q, nil))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("tg=%q = %d, want 400", q, rr.Code)
		}
	}
	rr := httptest.NewRecorder()
	(&radioHandlers{}).Live(rr, httptest.NewRequest(http.MethodGet, "/live", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("no live broker = %d, want 503", rr.Code)
	}
}
