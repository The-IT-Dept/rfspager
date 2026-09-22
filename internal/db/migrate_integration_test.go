package db

import (
	"context"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
)

// These tests intentionally use only RFSPAGER_TEST_DATABASE_URL. They never
// read application configuration or DATABASE_URL, which may point at a shared
// development database. Start the disposable PostGIS service documented in
// docs/database-migration.md before running them.
func testMigrationDB(t *testing.T) *DB {
	t.Helper()
	rawURL := os.Getenv("RFSPAGER_TEST_DATABASE_URL")
	if rawURL == "" {
		t.Skip("RFSPAGER_TEST_DATABASE_URL is not set")
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse RFSPAGER_TEST_DATABASE_URL: %v", err)
	}
	name := strings.TrimPrefix(u.Path, "/")
	if !strings.HasPrefix(name, "rfspager_test") {
		t.Fatalf("refusing to reset non-test database %q", name)
	}

	d, err := New(rawURL)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func resetMigrationDB(t *testing.T, d *DB) {
	t.Helper()
	ctx := context.Background()
	if _, err := d.conn.ExecContext(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatalf("reset test schema: %v", err)
	}
}

func requireTable(t *testing.T, d *DB, table string) {
	t.Helper()
	var exists bool
	if err := d.conn.GetContext(context.Background(), &exists, `SELECT to_regclass($1) IS NOT NULL`, "public."+table); err != nil {
		t.Fatalf("check table %s: %v", table, err)
	}
	if !exists {
		t.Errorf("table %s does not exist", table)
	}
}

// createRepresentativeLegacySchema models the Laravel tables that the first
// Go migrations alter. It is intentionally independent from the Go baseline:
// the upgrade test must prove that 000 leaves pre-existing rows and tables in
// place rather than merely re-testing a fresh bootstrap.
func createRepresentativeLegacySchema(t *testing.T, d *DB) {
	t.Helper()
	if _, err := d.conn.ExecContext(context.Background(), `
		CREATE TABLE users (
			id BIGSERIAL PRIMARY KEY, user_id UUID NOT NULL UNIQUE,
			name VARCHAR(255), email VARCHAR(255) NOT NULL UNIQUE,
			password VARCHAR(255) NOT NULL, subscription_valid BOOLEAN NOT NULL DEFAULT false,
			created_at TIMESTAMP NULL, updated_at TIMESTAMP NULL
		);
		CREATE TABLE systems (
			id BIGSERIAL PRIMARY KEY, system_id UUID NOT NULL,
			user_id BIGINT NOT NULL REFERENCES users(id), name VARCHAR(255) NOT NULL,
			deleted_at TIMESTAMP NULL, created_at TIMESTAMP NULL, updated_at TIMESTAMP NULL
		);
		CREATE TABLE capcodes (
			id BIGSERIAL PRIMARY KEY, address VARCHAR(255) NOT NULL,
			alias VARCHAR(255) NOT NULL, agency VARCHAR(255) NOT NULL,
			icon VARCHAR(255) NOT NULL, colour VARCHAR(255) NOT NULL, "ignore" BOOLEAN NOT NULL,
			deleted_at TIMESTAMP NULL, created_at TIMESTAMP NULL, updated_at TIMESTAMP NULL
		);
		CREATE TABLE messages (
			id BIGSERIAL PRIMARY KEY, system_id BIGINT NOT NULL REFERENCES systems(id),
			capcode_id BIGINT NULL REFERENCES capcodes(id), address VARCHAR(255) NOT NULL,
			message VARCHAR(255) NOT NULL, md5 VARCHAR(255) NOT NULL,
			deleted_at TIMESTAMP NULL, created_at TIMESTAMP NULL, updated_at TIMESTAMP NULL
		);
		CREATE TABLE notifications (
			id UUID PRIMARY KEY, type VARCHAR(255) NOT NULL,
			notifiable_type VARCHAR(255) NOT NULL, notifiable_id BIGINT NOT NULL,
			data TEXT NOT NULL, read_at TIMESTAMP NULL, created_at TIMESTAMP NULL, updated_at TIMESTAMP NULL
		);
		CREATE TABLE majorincidents (
			id BIGSERIAL PRIMARY KEY, title VARCHAR(255), upstream_id VARCHAR(255),
			category VARCHAR(255), geometry JSON, data JSON, created_at TIMESTAMP NULL, updated_at TIMESTAMP NULL
		);
		CREATE TABLE hazardreductions (
			id BIGSERIAL PRIMARY KEY, title VARCHAR(255), upstream_id VARCHAR(255),
			"leadAgency" VARCHAR(255), "supportingAgencies" VARCHAR(255), lga VARCHAR(255),
			size REAL, tenure VARCHAR(255), geometry JSON, start_at TIMESTAMP NULL, end_at TIMESTAMP NULL,
			created_at TIMESTAMP NULL, updated_at TIMESTAMP NULL
		);
	`); err != nil {
		t.Fatalf("create representative legacy schema: %v", err)
	}
}

func TestMigrateBootstrapsEmptyPostGISDatabase(t *testing.T) {
	d := testMigrationDB(t)
	resetMigrationDB(t, d)

	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate empty database: %v", err)
	}

	for _, table := range []string{
		"users", "systems", "capcodes", "messages", "installations", "notifications",
		"subscriptions", "filters", "alertzones", "responding", "photos",
		"majorincidents", "hazardreductions", "social_providers", "personal_access_tokens",
		"schema_migrations", "admin_users", "agencies", "capcode_suggestions",
		"iap_transactions", "system_connections", "system_spectrum_snapshot",
		"iap_subscription_ownership",
		"emergency_stations", "system_events", "system_logs",
	} {
		requireTable(t, d, table)
	}

	var hasPostGIS bool
	if err := d.conn.GetContext(context.Background(), &hasPostGIS, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'postgis')`); err != nil {
		t.Fatalf("check PostGIS extension: %v", err)
	}
	if !hasPostGIS {
		t.Fatal("PostGIS extension was not installed")
	}

	var applied int
	if err := d.conn.GetContext(context.Background(), &applied, `SELECT COUNT(*) FROM schema_migrations`); err != nil {
		t.Fatalf("count applied migrations: %v", err)
	}
	if applied < 22 { // 000 baseline plus the pre-auth Go migrations.
		t.Fatalf("applied %d migrations, want baseline and incremental migrations", applied)
	}
}

func TestMigrateUpgradesLegacyShapedDatabaseWithoutReplacingRows(t *testing.T) {
	d := testMigrationDB(t)
	resetMigrationDB(t, d)

	createRepresentativeLegacySchema(t, d)
	if _, err := d.conn.ExecContext(context.Background(), `
		INSERT INTO users (user_id, name, email, password, subscription_valid)
		VALUES ('11111111-1111-1111-1111-111111111111', 'Legacy User', 'legacy@example.test', 'legacy-hash', true);
		INSERT INTO systems (system_id, user_id, name)
		VALUES ('22222222-2222-2222-2222-222222222222', 1, 'Legacy System');
		INSERT INTO capcodes (address, alias, agency, icon, colour, "ignore")
		VALUES ('1234567', 'Legacy Capcode', 'RFS', 'fire', 'red', false);
		INSERT INTO messages (system_id, capcode_id, address, message, md5)
		VALUES (1, 1, '1234567', 'preserve this legacy page', 'legacy-md5');
	`); err != nil {
		t.Fatalf("seed representative legacy rows: %v", err)
	}

	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate legacy-shaped database: %v", err)
	}

	var got string
	if err := d.conn.GetContext(context.Background(), &got, `SELECT message FROM messages WHERE id = 1`); err != nil {
		t.Fatalf("read preserved legacy message: %v", err)
	}
	if got != "preserve this legacy page" {
		t.Fatalf("legacy message = %q, want preserved value", got)
	}
}

func TestApplyMigrationsRollsBackFailedMigrationAndBookkeeping(t *testing.T) {
	d := testMigrationDB(t)
	resetMigrationDB(t, d)

	err := d.applyMigrations(context.Background(), []migration{{
		name: "test_failed.sql",
		sql:  `CREATE TABLE migration_rollback_probe (id integer); SELECT 1 / 0;`,
	}})
	if err == nil {
		t.Fatal("failed migration unexpectedly succeeded")
	}

	var tableExists, versionExists bool
	if err := d.conn.GetContext(context.Background(), &tableExists, `SELECT to_regclass('public.migration_rollback_probe') IS NOT NULL`); err != nil {
		t.Fatalf("check rollback table: %v", err)
	}
	if err := d.conn.GetContext(context.Background(), &versionExists, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = 'test_failed.sql')`); err != nil {
		t.Fatalf("check rollback bookkeeping: %v", err)
	}
	if tableExists || versionExists {
		t.Fatalf("failed migration left state behind: table=%t version=%t", tableExists, versionExists)
	}
}

func TestApplyMigrationsSerializesConcurrentAttempts(t *testing.T) {
	d := testMigrationDB(t)
	resetMigrationDB(t, d)

	migrations := []migration{{
		name: "test_concurrent.sql",
		sql:  `SELECT pg_sleep(0.15); CREATE TABLE migration_concurrency_probe (id integer);`,
	}}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- d.applyMigrations(context.Background(), migrations)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent migration: %v", err)
		}
	}

	var count int
	if err := d.conn.GetContext(context.Background(), &count, `SELECT COUNT(*) FROM schema_migrations WHERE version = 'test_concurrent.sql'`); err != nil {
		t.Fatalf("count concurrent migration records: %v", err)
	}
	if count != 1 {
		t.Fatalf("migration was recorded %d times, want once", count)
	}

	var exists bool
	if err := d.conn.GetContext(context.Background(), &exists, `SELECT to_regclass('public.migration_concurrency_probe') IS NOT NULL`); err != nil {
		t.Fatalf("check concurrent migration table: %v", err)
	}
	if !exists {
		t.Fatal("concurrent migration did not create its table")
	}
}

func TestIAPOwnershipMigrationRejectsExistingDuplicateBindings(t *testing.T) {
	d := testMigrationDB(t)
	resetMigrationDB(t, d)
	if _, err := d.conn.ExecContext(context.Background(), `
		CREATE TABLE users (
			id BIGSERIAL PRIMARY KEY,
			apple_original_transaction_id TEXT,
			google_purchase_token TEXT
		);
		INSERT INTO users (apple_original_transaction_id) VALUES ('duplicate-apple'), ('duplicate-apple');
	`); err != nil {
		t.Fatalf("create duplicate ownership fixture: %v", err)
	}

	migration, err := migrationFS.ReadFile("migrations/023_iap_ownership.sql")
	if err != nil {
		t.Fatalf("read IAP ownership migration: %v", err)
	}
	_, err = d.conn.ExecContext(context.Background(), string(migration))
	if err == nil || !strings.Contains(err.Error(), "duplicate Apple subscription ownership") {
		t.Fatalf("duplicate ownership migration error = %v", err)
	}

	var uniqueIndexExists bool
	if err := d.conn.GetContext(context.Background(), &uniqueIndexExists,
		`SELECT to_regclass('public.users_apple_original_transaction_id_unique') IS NOT NULL`); err != nil {
		t.Fatalf("check ownership index after preflight failure: %v", err)
	}
	if uniqueIndexExists {
		t.Fatal("preflight failure left a partial ownership index behind")
	}
	var ledgerExists bool
	if err := d.conn.GetContext(context.Background(), &ledgerExists,
		`SELECT to_regclass('public.iap_subscription_ownership') IS NOT NULL`); err != nil {
		t.Fatalf("check ownership ledger after preflight failure: %v", err)
	}
	if ledgerExists {
		t.Fatal("preflight failure left an ownership ledger behind")
	}
}

func TestIAPOwnershipMigrationBackfillsCurrentBindings(t *testing.T) {
	d := testMigrationDB(t)
	resetMigrationDB(t, d)
	if _, err := d.conn.ExecContext(context.Background(), `
		CREATE TABLE users (
			id BIGSERIAL PRIMARY KEY,
			apple_original_transaction_id TEXT,
			google_purchase_token TEXT
		);
		INSERT INTO users (apple_original_transaction_id) VALUES ('apple-existing');
		INSERT INTO users (google_purchase_token) VALUES ('google-existing');
	`); err != nil {
		t.Fatalf("create ownership backfill fixture: %v", err)
	}

	migration, err := migrationFS.ReadFile("migrations/023_iap_ownership.sql")
	if err != nil {
		t.Fatalf("read IAP ownership migration: %v", err)
	}
	if _, err := d.conn.ExecContext(context.Background(), string(migration)); err != nil {
		t.Fatalf("apply IAP ownership migration: %v", err)
	}

	var owners int
	if err := d.conn.GetContext(context.Background(), &owners, `SELECT COUNT(*) FROM iap_subscription_ownership`); err != nil {
		t.Fatalf("count backfilled ownership rows: %v", err)
	}
	if owners != 2 {
		t.Fatalf("backfilled ownership rows = %d, want 2", owners)
	}
}

func TestMigrationPreservesDivergentDuplicateUpstreamRecords(t *testing.T) {
	d := testMigrationDB(t)
	resetMigrationDB(t, d)
	ctx := context.Background()
	baseline, err := migrationFS.ReadFile("migrations/000_bootstrap_legacy_schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.conn.ExecContext(ctx, string(baseline)); err != nil {
		t.Fatal(err)
	}
	if _, err := d.conn.ExecContext(ctx, `INSERT INTO majorincidents (upstream_id, title) VALUES ('duplicate-fixture', 'first record'), ('duplicate-fixture', 'different record')`); err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(ctx); err == nil || !strings.Contains(err.Error(), "duplicate majorincidents.upstream_id") {
		t.Fatalf("expected explicit duplicate preflight failure, got %v", err)
	}
	var count int
	if err := d.conn.GetContext(ctx, &count, `SELECT count(*) FROM majorincidents WHERE upstream_id='duplicate-fixture'`); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("migration lost legacy records: got %d want 2", count)
	}
}

func TestReceiverApprovalAndKeyRotation(t *testing.T) {
	d := testMigrationDB(t)
	resetMigrationDB(t, d)
	ctx := context.Background()
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err := d.conn.QueryRowContext(ctx, `INSERT INTO users (user_id,email,password) VALUES (gen_random_uuid(),'receiver-owner@example.invalid','!') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	var systemID int64
	if err := d.conn.QueryRowContext(ctx, `INSERT INTO systems (system_id,user_id,name,api_key,frequency,steal_capcodes,created_at,updated_at) VALUES (gen_random_uuid(),$1,'Test receiver','test-receiver-key',1,'0',NOW(),NOW()) RETURNING id`, userID).Scan(&systemID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetSystemByAPIKey(ctx, "test-receiver-key"); err == nil {
		t.Fatal("pending receiver authenticated")
	}
	if _, err := d.conn.ExecContext(ctx, `UPDATE systems SET approved_at=NOW() WHERE id=$1`, systemID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetSystemByAPIKey(ctx, "test-receiver-key"); err != nil {
		t.Fatalf("approved receiver rejected: %v", err)
	}
	if _, err := d.conn.ExecContext(ctx, `UPDATE systems SET api_key='rotated-test-receiver-key' WHERE id=$1`, systemID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetSystemByAPIKey(ctx, "test-receiver-key"); err == nil {
		t.Fatal("old key authenticated after rotation")
	}
	if _, err := d.GetSystemByAPIKey(ctx, "rotated-test-receiver-key"); err != nil {
		t.Fatalf("rotated key rejected: %v", err)
	}
	if _, err := d.conn.ExecContext(ctx, `UPDATE systems SET deleted_at=NOW() WHERE id=$1`, systemID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetSystemByAPIKey(ctx, "rotated-test-receiver-key"); err == nil {
		t.Fatal("deleted receiver authenticated")
	}
}
