package mobile

import (
	"context"
	"fmt"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInstallationCannotBeReassigned(t *testing.T) {
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO installations[\\s\\S]*WHERE installations.user_id = EXCLUDED.user_id").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	h := NewHandlers(sqlx.NewDb(conn, "sqlmock"))
	r := httptest.NewRequest("PUT", "/installations/11111111-1111-4111-8111-111111111111", strings.NewReader(`{"push_type":"GCM","push_token":"test-device-token"}`))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", "11111111-1111-4111-8111-111111111111")
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rc)
	ctx = context.WithValue(ctx, UserContextKey, &AuthUser{ID: 8})
	w := httptest.NewRecorder()
	h.UpsertInstallation(w, r.WithContext(ctx))
	if w.Code != 409 {
		t.Fatalf("foreign installation overwrite not rejected: %d", w.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestInstallationTokenLocksAreSharedSortedAndUnique(t *testing.T) {
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs("rfspager-installation-token:alpha").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs("rfspager-installation-token:zulu").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	db := sqlx.NewDb(conn, "sqlmock")
	tx, err := db.BeginTxx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := lockInstallationTokens(context.Background(), tx, "zulu", "alpha", "zulu"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// This uses a deliberately opt-in disposable PostgreSQL database. Advisory
// locks are PostgreSQL transaction semantics and cannot be meaningfully tested
// with sqlmock alone.
func TestInstallationConcurrentSharedTokenLeavesOneActiveRegistration(t *testing.T) {
	dsn := os.Getenv("RFSPAGER_TEST_INSTALLATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("set RFSPAGER_TEST_INSTALLATION_DATABASE_URL to run PostgreSQL installation concurrency test")
	}

	bootstrap, err := sqlx.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Close()
	schema := fmt.Sprintf("installation_race_%d", time.Now().UnixNano())
	if _, err := bootstrap.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = bootstrap.Exec(`DROP SCHEMA ` + schema + ` CASCADE`) })

	db, err := sqlx.Open("postgres", testDSNWithSearchPath(dsn, schema))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE installations (
		id UUID PRIMARY KEY, user_id BIGINT NOT NULL, push_type VARCHAR(255) NOT NULL,
		push_token VARCHAR(255), expo_token VARCHAR(255), vendor VARCHAR(255) NOT NULL,
		model VARCHAR(255) NOT NULL, version VARCHAR(255) NOT NULL, app_id VARCHAR(255) NOT NULL,
		app_name VARCHAR(255) NOT NULL, app_version VARCHAR(255) NOT NULL, timezone VARCHAR(255) NOT NULL,
		deleted_at TIMESTAMP NULL, created_at TIMESTAMP NULL, updated_at TIMESTAMP NULL
	)`); err != nil {
		t.Fatal(err)
	}

	h := NewHandlers(db)
	const token = "shared-synthetic-device-token"
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan int64, 2)
	for _, userID := range []int64{101, 202} {
		userID := userID
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if status := upsertInstallationForTest(h, userID, token); status != 200 {
				t.Errorf("user %d status=%d", userID, status)
				return
			}
			results <- userID
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	if len(results) != 2 {
		t.Fatal("concurrent registration failed")
	}

	var active int
	if err := db.Get(&active, `SELECT COUNT(*) FROM installations WHERE push_token = $1 AND deleted_at IS NULL`, token); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("active registrations=%d, want 1", active)
	}

	// A later registration deterministically wins after the concurrent race.
	if status := upsertInstallationForTest(h, 303, token); status != 200 {
		t.Fatalf("final registration status=%d", status)
	}
	var winner int64
	if err := db.Get(&winner, `SELECT user_id FROM installations WHERE push_token = $1 AND deleted_at IS NULL`, token); err != nil {
		t.Fatal(err)
	}
	if winner != 303 {
		t.Fatalf("last registration winner=%d, want 303", winner)
	}
}

func testDSNWithSearchPath(dsn, schema string) string {
	if parsed, err := url.Parse(dsn); err == nil && parsed.Scheme != "" {
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	return dsn + " search_path=" + schema
}

func upsertInstallationForTest(h *Handlers, userID int64, token string) int {
	installationID := fmt.Sprintf("00000000-0000-4000-8000-%012d", userID)
	r := httptest.NewRequest("PUT", "/installations/"+installationID, strings.NewReader(`{"push_type":"GCM","push_token":"`+token+`"}`))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", installationID)
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rc)
	ctx = context.WithValue(ctx, UserContextKey, &AuthUser{ID: userID})
	w := httptest.NewRecorder()
	h.UpsertInstallation(w, r.WithContext(ctx))
	return w.Code
}
func TestInstallationOwnerCanRefreshAndMoveOwnPushToken(t *testing.T) {
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO installations[\\s\\S]*WHERE installations.user_id = EXCLUDED.user_id").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE installations SET deleted_at = NOW()")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	h := NewHandlers(sqlx.NewDb(conn, "sqlmock"))
	r := httptest.NewRequest("PUT", "/", strings.NewReader(`{"push_type":"GCM","push_token":"test-device-token"}`))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", "11111111-1111-4111-8111-111111111111")
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rc)
	ctx = context.WithValue(ctx, UserContextKey, &AuthUser{ID: 8})
	w := httptest.NewRecorder()
	h.UpsertInstallation(w, r.WithContext(ctx))
	if w.Code != 200 {
		t.Fatalf("owner refresh rejected: %d", w.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
