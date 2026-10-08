package mobile

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/authjwt"
	"github.com/the-it-dept/rfspager/internal/radio"
)

func TestRadioStreamTokens(t *testing.T) {
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	db := sqlx.NewDb(conn, "sqlmock")
	router := RadioRouter(db, "test-secret", radio.NewStore(db), nil, nil, nil)
	do := func(method, path, bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		return rr
	}
	login, err := authjwt.Issue([]byte("test-secret"), "rfspager-mobile", "mobile", 1, 0, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}

	// A subscriber swaps their login token for a stream token.
	expectUser(mock, 1, true)
	rr := do(http.MethodPost, "/stream-token", login)
	if rr.Code != http.StatusOK {
		t.Fatalf("stream-token = %d %s", rr.Code, rr.Body)
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil || out.Token == "" {
		t.Fatalf("body %s: %v", rr.Body, err)
	}
	if d := time.Until(out.ExpiresAt); d < 14*time.Minute || d > 16*time.Minute {
		t.Errorf("expires in %s, want ~15m", d)
	}

	// It authenticates the audio route (404 here: no audio store is wired,
	// so reaching the handler means auth and the paywall both passed)...
	expectUser(mock, 1, true)
	if rr := do(http.MethodGet, "/calls/7/audio?token="+out.Token, ""); rr.Code != http.StatusNotFound {
		t.Errorf("audio via stream token = %d, want 404", rr.Code)
	}
	// ...but not the JSON API, which still requires the login token.
	if rr := do(http.MethodGet, "/calls?token="+out.Token, ""); rr.Code != http.StatusUnauthorized {
		t.Errorf("calls via stream token = %d, want 401", rr.Code)
	}
	// A login token doesn't work in the URL: only the scoped type does.
	if rr := do(http.MethodGet, "/calls/7/audio?token="+login, ""); rr.Code != http.StatusUnauthorized {
		t.Errorf("audio via login token in URL = %d, want 401", rr.Code)
	}
	// Stream tokens still respect the paywall when a subscription lapses.
	expectUser(mock, 1, false)
	if rr := do(http.MethodGet, "/calls/7/audio?token="+out.Token, ""); rr.Code != http.StatusPaymentRequired {
		t.Errorf("lapsed subscriber = %d, want 402: %s", rr.Code, rr.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
