package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/jmoiron/sqlx"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

const migrationAdvisoryLockKey int64 = 8_184_572_311_907_244

type migration struct {
	name string
	sql  string
}

// Migrate applies embedded migrations in lexicographic filename order. A
// PostgreSQL advisory lock serializes separate server processes, and each
// migration commits with its schema_migrations record in one transaction.
func (d *DB) Migrate(ctx context.Context) error {
	migrations, err := embeddedMigrations()
	if err != nil {
		return err
	}
	return d.applyMigrations(ctx, migrations)
}

func embeddedMigrations() ([]migration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("reading embedded migrations: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	migrations := make([]migration, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("duplicate migration name %q", name)
		}
		seen[name] = struct{}{}

		sql, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, fmt.Errorf("reading migration %s: %w", name, err)
		}
		if strings.TrimSpace(string(sql)) == "" {
			return nil, fmt.Errorf("migration %s is empty", name)
		}
		migrations = append(migrations, migration{name: name, sql: string(sql)})
	}
	return migrations, nil
}

func (d *DB) applyMigrations(ctx context.Context, migrations []migration) error {
	conn, err := d.conn.Connx(ctx)
	if err != nil {
		return fmt.Errorf("acquiring migration connection: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrationAdvisoryLockKey); err != nil {
		return fmt.Errorf("acquiring migration lock: %w", err)
	}
	defer func() {
		if _, err := conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationAdvisoryLockKey); err != nil {
			slog.Error("releasing migration lock", "error", err)
		}
	}()

	if err := ensureMigrationTable(ctx, conn); err != nil {
		return err
	}
	for _, migration := range migrations {
		if strings.TrimSpace(migration.name) == "" {
			return fmt.Errorf("migration has an empty name")
		}
		if strings.TrimSpace(migration.sql) == "" {
			return fmt.Errorf("migration %s is empty", migration.name)
		}
		if err := applyMigration(ctx, conn, migration); err != nil {
			return err
		}
	}
	return nil
}

func ensureMigrationTable(ctx context.Context, conn interface {
	BeginTxx(context.Context, *sql.TxOptions) (*sqlx.Tx, error)
}) error {
	// schema_migrations must exist before versions can be checked. The caller
	// holds the advisory lock, so this creation cannot race another migrator.
	tx, err := conn.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting migration bookkeeping transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing migration bookkeeping transaction: %w", err)
	}
	return nil
}

func applyMigration(ctx context.Context, conn interface {
	BeginTxx(context.Context, *sql.TxOptions) (*sqlx.Tx, error)
}, migration migration) error {
	tx, err := conn.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting migration %s: %w", migration.name, err)
	}
	defer tx.Rollback()

	var applied bool
	if err := tx.GetContext(ctx, &applied, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, migration.name); err != nil {
		return fmt.Errorf("checking migration %s: %w", migration.name, err)
	}
	if applied {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing skipped migration %s: %w", migration.name, err)
		}
		return nil
	}

	slog.Info("applying migration", "name", migration.name)
	if _, err := tx.ExecContext(ctx, migration.sql); err != nil {
		return fmt.Errorf("applying migration %s: %w", migration.name, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, migration.name); err != nil {
		return fmt.Errorf("recording migration %s: %w", migration.name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing migration %s: %w", migration.name, err)
	}
	slog.Info("migration applied", "name", migration.name)
	return nil
}
