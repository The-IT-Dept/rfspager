package public

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/the-it-dept/rfspager/internal/coverage"
)

type fakeCoverage struct {
	asked []int
	err   error
}

func (f *fakeCoverage) PublicReport(_ context.Context, days int) (*coverage.PublicResponse, error) {
	f.asked = append(f.asked, days)
	if f.err != nil {
		return nil, f.err
	}
	return &coverage.PublicResponse{
		Window:       coverage.Window{Days: coverage.PublicDays(days), Since: time.Unix(0, 0).UTC(), Until: time.Unix(0, 0).UTC()},
		Receivers:    []coverage.PublicReceiver{{Name: "Budgewoi", Lat: -33.23, Lng: 151.56}},
		Transmitters: []coverage.PublicTransmitter{},
		Districts:    []coverage.PublicDistrict{},
		Heat:         [][3]float64{},
	}, nil
}

func TestCoverageHandler(t *testing.T) {
	for query, wantDays := range map[string]int{"?days=30": 30, "?days=90": 90, "": 90, "?days=7": 90, "?days=abc": 90} {
		f := &fakeCoverage{}
		rec := httptest.NewRecorder()
		Coverage(f).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/public/coverage"+query, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%q: status %d", query, rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "public, max-age=900" {
			t.Errorf("%q: Cache-Control = %q", query, got)
		}
		var body coverage.PublicResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Window.Days != wantDays || len(body.Receivers) != 1 {
			t.Errorf("%q: body = %+v", query, body)
		}
	}
}

func TestCoverageHandlerHidesErrors(t *testing.T) {
	f := &fakeCoverage{err: errors.New("pq: relation page_receptions does not exist")}
	rec := httptest.NewRecorder()
	Coverage(f).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/public/coverage", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "" {
		t.Error("an error response was marked cacheable")
	}
	if body := rec.Body.String(); body == "" || !json.Valid([]byte(body)) || strings.Contains(body, "page_receptions") {
		t.Errorf("body = %s", body)
	}
}
