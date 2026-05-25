package importer

import (
	"context"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
)

type Importer struct {
	db       *sqlx.DB
	interval time.Duration
	stop     chan struct{}
}

func New(db *sqlx.DB, interval time.Duration) *Importer {
	return &Importer{db: db, interval: interval, stop: make(chan struct{})}
}

func (im *Importer) Start() {
	slog.Info("importer started", "interval", im.interval)

	// Run once immediately on startup
	im.runAll()

	ticker := time.NewTicker(im.interval)
	go func() {
		for {
			select {
			case <-ticker.C:
				im.runAll()
			case <-im.stop:
				ticker.Stop()
				slog.Info("importer stopped")
				return
			}
		}
	}()
}

func (im *Importer) Stop() {
	close(im.stop)
}

func (im *Importer) runAll() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	slog.Info("running imports")

	if err := importHazardReductions(ctx, im.db); err != nil {
		slog.Error("hazard reduction import failed", "error", err)
	}

	if err := importMajorIncidents(ctx, im.db); err != nil {
		slog.Error("major incidents import failed", "error", err)
	}
}
