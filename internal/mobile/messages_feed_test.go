package mobile

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

// callListMessages runs the feed handler against a driver that records the SQL
// it builds, and returns that SQL plus the bound arguments.
func callListMessages(t *testing.T, target string, db *sqlx.DB) *httptest.ResponseRecorder {
	t.Helper()
	h := &Handlers{db: db}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req = req.WithContext(context.WithValue(req.Context(), UserContextKey,
		&AuthUser{ID: 42, Email: "member@example.invalid", SubscriptionValid: true}))
	rr := httptest.NewRecorder()
	h.ListMessages(rr, req)
	return rr
}

func emptyFeedRows() *securityRows {
	return &securityRows{cols: []string{
		"id", "system_id", "capcode_id", "address", "message", "lat", "lng", "created_at",
		"capcode_alias", "capcode_icon", "capcode_colour", "agency_name",
		"responder_count", "am_i_responding",
	}}
}

// The feed paginates on the (created_at, id) tuple and must order by exactly
// that, or the sort returns and the cursor stops being stable.
func TestListMessagesPaginatesByKeysetCursorNotOffset(t *testing.T) {
	var gotSQL string
	var gotArgs []driver.NamedValue
	db := securityDB(t, &securityDriver{query: func(q string, a []driver.NamedValue) (driver.Rows, error) {
		gotSQL, gotArgs = q, a
		return emptyFeedRows(), nil
	}})

	rr := callListMessages(t,
		"/messages?per_page=25&before_created_at=2026-09-18T12:17:49%2B02:00&before_id=2474652", db)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	if !strings.Contains(gotSQL, "(m.created_at, m.id) <") {
		t.Errorf("cursor predicate missing:\n%s", gotSQL)
	}
	if !strings.Contains(gotSQL, "ORDER BY m.created_at DESC, m.id DESC") {
		t.Errorf("ORDER BY must match the cursor tuple:\n%s", gotSQL)
	}
	if strings.Contains(strings.ToUpper(gotSQL), "OFFSET") {
		t.Errorf("offset pagination must not come back:\n%s", gotSQL)
	}

	// A cursor that fails to parse is dropped silently, which hands the reader
	// page 1 again — so prove both halves were understood and bound.
	var sawTime, sawID bool
	for _, a := range gotArgs {
		if _, ok := a.Value.(time.Time); ok {
			sawTime = true
		}
		if v, ok := a.Value.(int64); ok && v == 2474652 {
			sawID = true
		}
	}
	if !sawTime {
		t.Error("cursor timestamp was not parsed and bound")
	}
	if !sawID {
		t.Error("cursor id was not bound")
	}
}

// Placeholders are derived from len(args) so they cannot drift from their
// values. That invariant is worth pinning: a desync is a runtime 500.
func TestListMessagesBindsEveryPlaceholderItUses(t *testing.T) {
	var gotSQL string
	var gotArgs []driver.NamedValue
	db := securityDB(t, &securityDriver{query: func(q string, a []driver.NamedValue) (driver.Rows, error) {
		gotSQL, gotArgs = q, a
		return emptyFeedRows(), nil
	}})

	callListMessages(t, "/messages?per_page=10&capcode_id=7&show_only_filtered=true"+
		"&search=structure+fire&created_after=2026-01-01T00:00:00Z"+
		"&before_created_at=2026-09-18T12:17:49Z&before_id=99", db)

	highest := 0
	for i := 1; i <= 64; i++ {
		if strings.Contains(gotSQL, "$"+strconv.Itoa(i)) {
			highest = i
		}
	}
	if highest == 0 {
		t.Fatalf("no placeholders in:\n%s", gotSQL)
	}
	if highest != len(gotArgs) {
		t.Errorf("highest placeholder $%d but %d args bound:\n%s", highest, len(gotArgs), gotSQL)
	}
}

// Search must go through the GIN-indexed tsvector. ILIKE '%term%' cannot use an
// index — a term with no matches walked the whole feed and took 3.7s.
func TestListMessagesSearchUsesFullTextIndexNotIlike(t *testing.T) {
	var gotSQL string
	db := securityDB(t, &securityDriver{query: func(q string, a []driver.NamedValue) (driver.Rows, error) {
		gotSQL = q
		return emptyFeedRows(), nil
	}})

	callListMessages(t, "/messages?search=structure+fire", db)

	if !strings.Contains(gotSQL, "websearch_to_tsquery") {
		t.Errorf("search must use the tsvector index:\n%s", gotSQL)
	}
	if strings.Contains(strings.ToUpper(gotSQL), "ILIKE") {
		t.Errorf("ILIKE must not come back:\n%s", gotSQL)
	}
	if !strings.Contains(gotSQL, "m.address LIKE") {
		t.Errorf("capcode address lookups must still work:\n%s", gotSQL)
	}
}

// The unknown-capcode feed is a paid opt-in and is the inverse predicate, so it
// needs its own partial index; make sure the two paths stay distinct. Each case
// is a subtest because the shared harness registers its driver under t.Name().
func TestListMessagesUnknownFeedIsOptInAndInverted(t *testing.T) {
	for _, tc := range []struct {
		name       string
		target     string
		subscribed bool
		want       string
	}{
		{"default feed hides unknown capcodes", "/messages", true, "m.capcode_id IS NOT NULL"},
		{"paid opt-in shows only unknown", "/messages?show_unknown=true", true, "m.capcode_id IS NULL"},
		{"unpaid opt-in is ignored", "/messages?show_unknown=true", false, "m.capcode_id IS NOT NULL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotSQL string
			db := securityDB(t, &securityDriver{query: func(q string, a []driver.NamedValue) (driver.Rows, error) {
				gotSQL = q
				return emptyFeedRows(), nil
			}})
			h := &Handlers{db: db}
			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			req = req.WithContext(context.WithValue(req.Context(), UserContextKey,
				&AuthUser{ID: 42, SubscriptionValid: tc.subscribed}))
			h.ListMessages(httptest.NewRecorder(), req)

			if !strings.Contains(gotSQL, tc.want) {
				t.Errorf("expected %q in:\n%s", tc.want, gotSQL)
			}
		})
	}
}

// The client builds the next cursor from created_at as the server serialised it.
// If that format ever stops satisfying RFC3339Nano the server ignores the cursor
// and returns page 1 forever, which looks like a feed that repeats itself.
func TestFeedTimestampSurvivesTheCursorRoundTrip(t *testing.T) {
	for _, want := range []time.Time{
		time.Date(2026, 9, 18, 12, 17, 49, 0, time.FixedZone("AEST", 10*3600)), // no fractional part
		time.Date(2026, 9, 18, 12, 17, 49, 123456000, time.UTC),                // microseconds, as Postgres returns
		time.Date(2022, 8, 1, 1, 37, 6, 0, time.UTC),
	} {
		encoded, err := json.Marshal(map[string]any{"created_at": want})
		if err != nil {
			t.Fatal(err)
		}
		var back map[string]string
		if err := json.Unmarshal(encoded, &back); err != nil {
			t.Fatal(err)
		}
		got, err := time.Parse(time.RFC3339Nano, back["created_at"])
		if err != nil {
			t.Fatalf("server emits %q, which the cursor parser rejects: %v", back["created_at"], err)
		}
		if !got.Equal(want) {
			t.Errorf("round trip moved the instant: %s -> %s", want, got)
		}
	}
}
