// migrate-spike applies pending SQL migrations and exits.
//
// cmd/server runs Migrate() and then immediately starts the fanout and dispatch
// workers, so booting it is not a safe way to apply migrations to a database
// that still has live push tokens. This binary runs the same Migrate() against
// DATABASE_URL and nothing else — no workers, no notifier, no importers — so
// migrations can be applied deliberately ahead of a deploy.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/the-it-dept/rfspager/internal/db"
)

func main() {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL required")
		os.Exit(1)
	}
	d, err := db.New(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}
	start := time.Now()
	if err := d.Migrate(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "MIGRATE FAILED:", err)
		os.Exit(1)
	}
	fmt.Printf("MIGRATE OK in %s\n", time.Since(start).Round(time.Millisecond))
}
