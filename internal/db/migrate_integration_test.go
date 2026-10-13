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

func requireNoTable(t *testing.T, d *DB, table string) {
	t.Helper()
	var exists bool
	if err := d.conn.GetContext(context.Background(), &exists, `SELECT to_regclass($1) IS NOT NULL`, "public."+table); err != nil {
		t.Fatalf("check table %s: %v", table, err)
	}
	if exists {
		t.Errorf("table %s still exists", table)
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
			-- Laravel's create_users_table ships these two as well; 023 reads
			-- them when auditing released ownership claims.
			subscription_expiry TIMESTAMP NULL, subscription_product_id VARCHAR(255) NULL,
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
		"iap_transactions", "system_connections",
		"iap_subscription_ownership",
		"emergency_stations", "system_events", "system_logs",
		"receiver_config_revisions", "system_stats", "system_commands", "system_captures",
	} {
		requireTable(t, d, table)
	}

	// 015 created the waterfall snapshot table; 025 drops it again now that
	// the spectrum feature is gone. Assert the drop actually applied.
	requireNoTable(t, d, "system_spectrum_snapshot")

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

// Production reaches 023 with one person holding several accounts that all
// restored the same subscription, so the duplicates are not contested. The
// migration de-duplicates the pointer and must leave every entitlement alone.
// iapOwnershipFixture is the slice of the real schema that migration 023
// touches: the user pointers it de-duplicates, the installations it ranks by,
// and the audit table it writes released claims to.
const iapOwnershipFixture = `
	CREATE TABLE users (
		id BIGSERIAL PRIMARY KEY,
		apple_original_transaction_id TEXT,
		google_purchase_token TEXT,
		subscription_valid BOOLEAN NOT NULL DEFAULT false,
		subscription_expiry TIMESTAMPTZ,
		subscription_product_id TEXT,
		last_store_sync_at TIMESTAMPTZ,
		updated_at TIMESTAMPTZ
	);
	CREATE TABLE installations (
		id BIGSERIAL PRIMARY KEY,
		user_id BIGINT NOT NULL,
		updated_at TIMESTAMPTZ,
		deleted_at TIMESTAMPTZ
	);
	CREATE TABLE iap_transactions (
		id BIGSERIAL PRIMARY KEY,
		user_id BIGINT NOT NULL,
		platform VARCHAR(16) NOT NULL,
		transaction_id VARCHAR(255),
		original_transaction_id VARCHAR(255),
		product_id VARCHAR(255) NOT NULL,
		event_type VARCHAR(64) NOT NULL,
		is_valid BOOLEAN NOT NULL,
		expires_at TIMESTAMPTZ,
		will_renew BOOLEAN,
		environment VARCHAR(32),
		raw_payload TEXT,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
`

func TestIAPOwnershipMigrationReconcilesDuplicateBindingsWithoutRevokingAccess(t *testing.T) {
	d := testMigrationDB(t)
	resetMigrationDB(t, d)
	if _, err := d.conn.ExecContext(context.Background(), iapOwnershipFixture+`
		-- Three accounts sharing one Apple subscription. Only 102 has a live
		-- install, so it is the account actually in use and must win.
		INSERT INTO users (id, apple_original_transaction_id, subscription_valid, subscription_expiry, subscription_product_id)
		VALUES (101, 'duplicate-apple', true, NOW() + INTERVAL '30 days', 'monthly'),
		       (102, 'duplicate-apple', true, NOW() + INTERVAL '30 days', 'monthly'),
		       (103, 'duplicate-apple', true, NOW() + INTERVAL '30 days', 'monthly');
		INSERT INTO installations (user_id, updated_at, deleted_at) VALUES (102, NOW(), NULL);
		-- A soft-deleted install must not win it for 103.
		INSERT INTO installations (user_id, updated_at, deleted_at) VALUES (103, NOW(), NOW());

		-- Two accounts sharing a Google token, neither with an install: the
		-- oldest id is the deterministic tie-break.
		INSERT INTO users (id, google_purchase_token, subscription_valid, subscription_expiry, subscription_product_id)
		VALUES (201, 'duplicate-google', true, NOW() + INTERVAL '10 days', 'yearly'),
		       (202, 'duplicate-google', true, NOW() + INTERVAL '10 days', 'yearly');
	`); err != nil {
		t.Fatalf("create duplicate ownership fixture: %v", err)
	}

	migration, err := migrationFS.ReadFile("migrations/023_iap_ownership.sql")
	if err != nil {
		t.Fatalf("read IAP ownership migration: %v", err)
	}
	if _, err := d.conn.ExecContext(context.Background(), string(migration)); err != nil {
		t.Fatalf("apply IAP ownership migration: %v", err)
	}

	// The account with the live install keeps the Apple binding; the others lose
	// only the pointer.
	for id, wantOwner := range map[int64]bool{101: false, 102: true, 103: false} {
		var holds bool
		if err := d.conn.GetContext(context.Background(), &holds,
			`SELECT apple_original_transaction_id IS NOT NULL FROM users WHERE id = $1`, id); err != nil {
			t.Fatalf("read apple binding for %d: %v", id, err)
		}
		if holds != wantOwner {
			t.Errorf("user %d holds apple binding = %v, want %v", id, holds, wantOwner)
		}
	}
	var googleOwner int64
	if err := d.conn.GetContext(context.Background(), &googleOwner,
		`SELECT id FROM users WHERE google_purchase_token IS NOT NULL`); err != nil {
		t.Fatalf("read google owner: %v", err)
	}
	if googleOwner != 201 {
		t.Errorf("google owner = %d, want 201 (oldest id wins the tie-break)", googleOwner)
	}

	// The whole point: no one is downgraded.
	var stillValid int
	if err := d.conn.GetContext(context.Background(), &stillValid,
		`SELECT COUNT(*) FROM users WHERE subscription_valid`); err != nil {
		t.Fatalf("count valid subscriptions: %v", err)
	}
	if stillValid != 5 {
		t.Errorf("users still subscription_valid = %d, want 5 (reconciliation must not revoke access)", stillValid)
	}

	// One ledger row per identifier, pointing at the winner.
	var ledger int
	if err := d.conn.GetContext(context.Background(), &ledger,
		`SELECT COUNT(*) FROM iap_subscription_ownership`); err != nil {
		t.Fatalf("count ledger rows: %v", err)
	}
	if ledger != 2 {
		t.Errorf("ledger rows = %d, want 2", ledger)
	}
	var appleLedgerOwner int64
	if err := d.conn.GetContext(context.Background(), &appleLedgerOwner,
		`SELECT user_id FROM iap_subscription_ownership WHERE platform = 'apple'`); err != nil {
		t.Fatalf("read apple ledger owner: %v", err)
	}
	if appleLedgerOwner != 102 {
		t.Errorf("apple ledger owner = %d, want 102", appleLedgerOwner)
	}

	// Support needs to know which accounts gave up a claim.
	var released int
	if err := d.conn.GetContext(context.Background(), &released,
		`SELECT COUNT(*) FROM iap_transactions WHERE event_type = 'reconcile:ownership_released'`); err != nil {
		t.Fatalf("count released-claim audit rows: %v", err)
	}
	if released != 3 {
		t.Errorf("released-claim audit rows = %d, want 3 (users 101, 103, 202)", released)
	}
}

func TestIAPOwnershipMigrationBackfillsCurrentBindings(t *testing.T) {
	d := testMigrationDB(t)
	resetMigrationDB(t, d)
	if _, err := d.conn.ExecContext(context.Background(), iapOwnershipFixture+`
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
