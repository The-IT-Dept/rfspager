package importer

import (
	"context"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
)

// stationImportCooldown is how long we wait between station refreshes. The GA
// source updates "as needed" (typically yearly) and FRNSW publishes a daily
// regenerated GeoRSS, so daily is plenty.
const stationImportCooldown = 24 * time.Hour

type Importer struct {
	db                *sqlx.DB
	interval          time.Duration
	stop              chan struct{}
	lastStationImport time.Time
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

	// Stations are slow-changing — refresh at most once per day. The GA EMFD
	// payload is ~12 MB so we don't want to hit it every 5 minutes.
	if im.lastStationImport.IsZero() || time.Since(im.lastStationImport) >= stationImportCooldown {
		stationsCtx, stationsCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		if err := importStations(stationsCtx, im.db); err != nil {
			slog.Error("stations import failed", "error", err)
		} else {
			im.lastStationImport = time.Now()
		}
		stationsCancel()
	}
}
