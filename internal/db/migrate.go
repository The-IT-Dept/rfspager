package db

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

func (d *DB) Migrate(ctx context.Context) error {
	// Create tracking table
	if _, err := d.conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	// Get already-applied versions
	applied := map[string]bool{}
	rows, err := d.conn.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("reading schema_migrations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		rows.Scan(&v)
		applied[v] = true
	}

	// Read embedded migration files
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("reading embedded migrations: %w", err)
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	// Apply unapplied migrations in order
	for _, name := range names {
		if applied[name] {
			continue
		}

		sql, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", name, err)
		}

		slog.Info("applying migration", "name", name)
		if _, err := d.conn.ExecContext(ctx, string(sql)); err != nil {
			return fmt.Errorf("applying migration %s: %w", name, err)
		}

		if _, err := d.conn.ExecContext(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			return fmt.Errorf("recording migration %s: %w", name, err)
		}

		slog.Info("migration applied", "name", name)
	}

	return nil
}
