// Command migrate applies database schema changes without starting HTTP,
// receiver, notification, or importer workers.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/the-it-dept/rfspager/internal/db"
)

func main() {
	databaseURL := flag.String("database-url", os.Getenv("DATABASE_URL"), "PostgreSQL connection URL")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *databaseURL); err != nil {
		slog.Error("database migration failed", "error", err)
		os.Exit(1)
	}
	slog.Info("database migrations complete")
}

func run(ctx context.Context, databaseURL string) error {
	if databaseURL == "" {
		return errors.New("database URL is required")
	}
	database, err := db.New(databaseURL)
	if err != nil {
		return err
	}
	defer database.Close()
	if err := database.Ping(ctx); err != nil {
		return err
	}
	return database.Migrate(ctx)
}
