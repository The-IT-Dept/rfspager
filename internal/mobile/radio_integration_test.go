package mobile

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/authjwt"
	"github.com/the-it-dept/rfspager/internal/db"
	"github.com/the-it-dept/rfspager/internal/p25"
	"github.com/the-it-dept/rfspager/internal/radio"
)

func radioIntegrationDB(t *testing.T) (*sqlx.DB, string) {
	t.Helper()
	raw := os.Getenv("RFSPAGER_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("RFSPAGER_TEST_DATABASE_URL is not set")
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.HasPrefix(strings.TrimPrefix(u.Path, "/"), "rfspager_test") {
		t.Fatalf("refusing to reset non-test database %q", raw)
	}
	conn, err := sqlx.Connect("postgres", raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := conn.Exec(`
		DROP SCHEMA public CASCADE;
		CREATE SCHEMA public;
		CREATE TABLE users (
			id BIGSERIAL PRIMARY KEY,
			user_id UUID NOT NULL DEFAULT gen_random_uuid(),
			name TEXT,
			email TEXT NOT NULL UNIQUE,
			token_version BIGINT NOT NULL DEFAULT 0,
			subscription_valid BOOLEAN NOT NULL DEFAULT false,
			subscription_product_id TEXT,
			subscription_expiry TEXT,
			will_renew BOOLEAN,
			subscription_platform TEXT,
			superuser BOOLEAN NOT NULL DEFAULT false
		);
		CREATE TABLE systems (id BIGSERIAL PRIMARY KEY);
		INSERT INTO systems DEFAULT VALUES;
		INSERT INTO users (email, subscription_valid) VALUES
			('free@example.invalid', false),
			('paid@example.invalid', true);
	`); err != nil {
		t.Fatalf("schema: %v", err)
	}
	for _, name := range []string{"032_radio.sql", "033_radio_audio.sql", "034_radio_patches.sql"} {
		migration, err := os.ReadFile("../db/migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(string(migration)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	return conn, raw
}

// End to end through the real middleware, LISTEN/NOTIFY and broker, on a
// server whose WriteTimeout is far shorter than the stream is held open: the
// stream must outlive it, as it must outlive the 30s one in production.
func TestRadioAPIRequiresSubscriptionAndStreamsPastWriteTimeout(t *testing.T) {
	conn, raw := radioIntegrationDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	store := radio.NewStore(conn)
	// Subscribers only get public agencies' talkgroups (radio/visibility.go).
	if _, err := conn.Exec(`INSERT INTO radio_talkgroups (sysid, tgid, alpha_tag, agency) VALUES ($1, 10047, 'RFS CENCO A', 'RFS')`, radio.DefaultSysID); err != nil {
		t.Fatal(err)
	}
	broker := radio.NewBroker(store)
	listener, err := db.NewListener(raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close(context.Background()) })
	wake, err := listener.Listen("radio_call", 16)
	if err != nil {
		t.Fatal(err)
	}
	go broker.Run(ctx, wake)

	r := chi.NewRouter()
	r.Mount("/radio", RadioRouter(conn, "test-secret", store, broker, nil, nil))
	srv := httptest.NewUnstartedServer(r)
	srv.Config.WriteTimeout = time.Second
	srv.Start()
	t.Cleanup(srv.Close)

	token := func(userID int64) string {
		tok, err := authjwt.Issue([]byte("test-secret"), "rfspager-mobile", "mobile", userID, 0, time.Hour, nil)
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	get := func(path, tok string) *http.Response {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	resp := get("/radio/calls", token(1))
	resp.Body.Close()
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("free user GET /calls = %d, want 402", resp.StatusCode)
	}

	stream := get("/radio/events", token(2))
	defer stream.Body.Close()
	if stream.StatusCode != http.StatusOK {
		t.Fatalf("subscriber GET /events = %d", stream.StatusCode)
	}
	events := make(chan string, 8)
	go func() {
		sc := bufio.NewScanner(stream.Body)
		for sc.Scan() {
			if line := sc.Text(); strings.HasPrefix(line, "data: ") {
				events <- strings.TrimPrefix(line, "data: ")
			}
		}
		close(events)
	}()

	time.Sleep(1500 * time.Millisecond) // past the server's WriteTimeout
	start := time.Now().Add(-time.Second)
	if _, err := store.StartCall(ctx, 1, p25.CallPayload{
		CallID: "sse-1", SysID: radio.DefaultSysID, RFSS: 1, Site: 51, Group: 10047, Source: 2039407,
		StartMS: start.UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case data, ok := <-events:
		if !ok {
			t.Fatal("stream ended before the call arrived (cut by the write timeout?)")
		}
		var call radio.Call
		if err := json.Unmarshal([]byte(data), &call); err != nil || call.TGID == nil || *call.TGID != 10047 || call.EndedAt != nil {
			t.Fatalf("event = %s (%v)", data, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no call event within 5s")
	}

	resp = get("/radio/calls?tg=10047", token(2))
	var page struct {
		Data    []radio.Call `json:"data"`
		HasMore bool         `json:"has_more"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /calls = %d (%v)", resp.StatusCode, err)
	}
	resp.Body.Close()
	if len(page.Data) != 1 || page.HasMore {
		t.Errorf("calls page = %+v", page)
	}

	resp = get("/radio/units/2039407", token(2))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /units/2039407 = %d", resp.StatusCode)
	}
	resp = get("/radio/calls?tg=abc", token(2))
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("GET /calls?tg=abc = %d, want 400", resp.StatusCode)
	}
}
