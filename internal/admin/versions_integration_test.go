package admin

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func TestGetAppVersionStats(t *testing.T) {
	rawURL := os.Getenv("RFSPAGER_TEST_DATABASE_URL")
	if rawURL == "" {
		t.Skip("RFSPAGER_TEST_DATABASE_URL is not set")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse RFSPAGER_TEST_DATABASE_URL: %v", err)
	}
	if !strings.HasPrefix(strings.TrimPrefix(u.Path, "/"), "rfspager_test") {
		t.Fatalf("refusing to reset non-test database %q", u.Path)
	}
	db, err := sqlx.Connect("postgres", rawURL)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
		DROP SCHEMA public CASCADE;
		CREATE SCHEMA public;
		CREATE TABLE installations (
			id BIGSERIAL PRIMARY KEY, user_id BIGINT, app_version TEXT, push_type TEXT,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), deleted_at TIMESTAMPTZ
		);
		INSERT INTO installations (user_id, app_version, push_type, updated_at, deleted_at) VALUES
			(1, '4.1.0', 'APN', NOW(), NULL),
			(1, '5.3.8', 'APN', NOW(), NULL),      -- same account on both apps
			(2, '4.1.0', 'GCM', NOW(), NULL),
			(3, '5.10.0', 'GCM', NOW(), NULL),
			(4, 'beta-x', 'GCM', NOW(), NULL),     -- unparseable: old app, no error
			(5, '', 'APN', NOW(), NULL),
			(6, '5.3.8', 'APN', NOW() - interval '40 days', NULL), -- inactive
			(7, '5.3.8', 'APN', NOW(), NOW());     -- deleted
	`); err != nil {
		t.Fatal(err)
	}

	stats, err := NewStore(db).GetAppVersionStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.ActiveDevices != 6 || stats.NewAppDevices != 2 || stats.ActiveAccounts != 5 ||
		stats.NewAppAccounts != 2 || stats.RegisteredDevices != 7 {
		t.Fatalf("summary = %+v", stats)
	}
	var got []string
	for _, v := range stats.Versions {
		got = append(got, v.Version)
	}
	if want := "5.10.0,5.3.8,4.1.0,unknown,beta-x"; strings.Join(got, ",") != want {
		t.Fatalf("versions = %v, want %s", got, want)
	}
	if v := stats.Versions[2]; v.IOS != 1 || v.Android != 1 || v.Devices != 2 || v.Accounts != 2 {
		t.Fatalf("4.1.0 row = %+v", v)
	}
}
