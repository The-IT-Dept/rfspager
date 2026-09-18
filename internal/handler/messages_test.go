package handler

import (
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The real PagerMon reader.js — the client every receiver runs — posts
// form-urlencoded. Before this was handled, ProcessPagermon's JSON-only
// decode 400'd every one of them.
func TestDecodePagermonBody(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		want        pagermonBody
	}{
		{
			name:        "reader.js form-urlencoded",
			contentType: "application/x-www-form-urlencoded",
			body:        "address=0521839&message=FRINC+TYPE%3A+YARD+FIRE&datetime=1758200000&source=rfspager-raymond-terrace-1+%28FRNSW%29",
			want:        pagermonBody{Address: "0521839", Message: "FRINC TYPE: YARD FIRE", Datetime: "1758200000"},
		},
		{
			name:        "form body carrying the key",
			contentType: "application/x-www-form-urlencoded",
			body:        "address=1&message=hi&apikey=ABC123",
			want:        pagermonBody{Address: "1", Message: "hi", Datetime: "", APIKey: "ABC123"},
		},
		{
			name:        "SimpleWebhook json",
			contentType: "application/json",
			body:        `{"address":"0521839","message":"FRINC","datetime":"1758200000"}`,
			want:        pagermonBody{Address: "0521839", Message: "FRINC", Datetime: "1758200000"},
		},
		{
			name:        "json with numeric datetime",
			contentType: "application/json",
			body:        `{"address":"7","message":"m","datetime":1758200000}`,
			want:        pagermonBody{Address: "7", Message: "m", Datetime: float64(1758200000)},
		},
		{
			// Clients lie about Content-Type; we dispatch on the payload shape.
			name:        "form body mislabelled as json",
			contentType: "application/json",
			body:        "address=9&message=m",
			want:        pagermonBody{Address: "9", Message: "m", Datetime: ""},
		},
		{
			name:        "json body with no content-type",
			contentType: "",
			body:        `{"address":"9","message":"m"}`,
			want:        pagermonBody{Address: "9", Message: "m"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/messages", strings.NewReader(tc.body))
			if tc.contentType != "" {
				r.Header.Set("Content-Type", tc.contentType)
			}
			got, err := decodePagermonBody(r)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.Address != tc.want.Address || got.Message != tc.want.Message || got.APIKey != tc.want.APIKey {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
			if tc.want.Datetime != nil && got.Datetime != tc.want.Datetime {
				t.Errorf("datetime: got %v (%T), want %v (%T)", got.Datetime, got.Datetime, tc.want.Datetime, tc.want.Datetime)
			}
		})
	}
}

func TestDecodePagermonBodyRejectsGarbageJSON(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/messages", strings.NewReader(`{"address":`))
	r.Header.Set("Content-Type", "application/json")
	if _, err := decodePagermonBody(r); err == nil {
		t.Fatal("expected an error for truncated JSON")
	}
}

// Receivers with a dead clock have written rows dated year 8425. A future
// timestamp sails past the "too old" guard in createMessage, so it is clamped
// at parse time; backdated ones are left for that guard to reject.
func TestParseTimestampClampsFutureOnly(t *testing.T) {
	now := time.Now()

	if got := parseTimestamp("203000000000"); got.After(now.Add(2 * time.Hour)) {
		t.Errorf("year-8425 epoch was not clamped: got %s", got)
	}

	stale := now.Add(-48 * time.Hour).Unix()
	got := parseTimestamp(strconv.FormatInt(stale, 10))
	if got.After(now.Add(-24 * time.Hour)) {
		t.Errorf("backdated timestamp should be preserved for the too-old guard, got %s", got)
	}

	fresh := now.Add(-30 * time.Second).Unix()
	if got := parseTimestamp(strconv.FormatInt(fresh, 10)); got.Unix() != fresh {
		t.Errorf("normal timestamp altered: got %d want %d", got.Unix(), fresh)
	}

	if got := parseTimestamp(""); got.Before(now.Add(-time.Minute)) {
		t.Errorf("empty timestamp should fall back to now, got %s", got)
	}
}
