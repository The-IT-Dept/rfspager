# Database migration procedure

The Go server and the migration command use the same embedded migration set.
Run the migration command as a separate cutover step; it does not start HTTP
listeners, receivers, importers, or notification workers.

The migrations require PostgreSQL with PostGIS available. The database role
must be allowed to create the `postgis` and `pgcrypto` extensions on a new
database, or an administrator must install those extensions before the first
run. The first migration creates only sanitized schema: it contains no users,
administrators, receiver registrations, API keys, or other operational data.

For an existing Laravel database, take and verify a backup, then run the
migration command in a maintenance window:

```sh
DATABASE_URL='postgres://migration-role@db.example/rfspager?sslmode=require' \
  go run ./cmd/migrate
```

The baseline uses `CREATE ... IF NOT EXISTS`, so it leaves existing Laravel
tables and rows in place. Later migrations apply their normal additive or
data-maintenance work. In particular, migration 012 converts legacy naive
timestamps assuming their stored values represent UTC; review its lock and
duration notes before scheduling it against a large database.

Migration 023 snapshots Apple original transaction IDs and Google purchase
tokens into a durable ownership ledger, while keeping the active user pointers
unique. It stops with a clear preflight error if legacy data binds one store
entitlement to more than one user. Resolve those records with store evidence
before retrying; the migration never picks a winner or moves a purchase
between accounts.

Do not start the Go server until this command has succeeded. Start receiver,
importer, and notification workers only after the schema cutover and
configuration review are complete.

For a disposable local PostGIS instance:

```sh
docker compose up -d postgres
export RFSPAGER_TEST_DATABASE_URL='postgres://postgres@127.0.0.1:55432/rfspager_test?sslmode=disable'
go test ./internal/db
DATABASE_URL="$RFSPAGER_TEST_DATABASE_URL" go run ./cmd/migrate
docker compose down
```

`RFSPAGER_TEST_DATABASE_URL` is used solely by the integration tests. It must
name a database beginning with `rfspager_test`; the tests reset its `public`
schema and intentionally never read `.env` or `DATABASE_URL`.

Before applying migration 002, reconcile duplicate non-null `upstream_id`
values in `majorincidents` and `hazardreductions` on a reviewed database copy.
The migration refuses duplicates and preserves the conflicting records; it does
not pick a winner or delete incident data. Record the intended reconciliation
before making any operator-approved data changes.

Run destructive fixture suites sequentially (`go test -p 1`) when the db and
iap packages share the same disposable database. Never run them concurrently.
