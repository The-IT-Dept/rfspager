package coverage

import (
	"context"
	"database/sql"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/the-it-dept/rfspager/internal/importer"
)

const (
	// receptionRetention is how long page_receptions rows are kept.
	receptionRetention = 400 * 24 * time.Hour
	// importMaxAge is how stale the ACMA import may get before the daily
	// check refreshes it. ACMA publishes weekly.
	importMaxAge = 7 * 24 * time.Hour
	// maintenanceInterval is the daily import check, district recompute and
	// reception prune.
	maintenanceInterval = 24 * time.Hour
	// maintenanceStartDelay keeps the first pass clear of boot (migrations,
	// receiver reconnect storm).
	maintenanceStartDelay = 2 * time.Minute
	// importTimeout bounds a download + parse + upsert of the register.
	importTimeout = 30 * time.Minute
)

// Options configures a Service.
type Options struct {
	// ACMAURL is the register zip; empty uses importer.DefaultACMARRLURL.
	ACMAURL string
	// ImportDisabled stops the scheduled ACMA import. A refresh an admin
	// asks for still runs.
	ImportDisabled bool
}

// Service owns the coverage map: report building and caching, the ACMA
// transmitter import, district assignment and reception retention.
type Service struct {
	db           *sqlx.DB
	opts         Options
	reports      *ttlCache[Params, *Response]
	globalsCache *ttlCache[struct{}, globals]
	importing    atomic.Bool
	now          func() time.Time
}

func NewService(db *sqlx.DB, opts Options) *Service {
	return &Service{
		db:           db,
		opts:         opts,
		reports:      newTTLCache[Params, *Response](cacheTTL),
		globalsCache: newTTLCache[struct{}, globals](cacheTTL),
		now:          time.Now,
	}
}

// Report returns the coverage report for p (already normalised), from cache
// when one was built in the last cacheTTL.
func (s *Service) Report(ctx context.Context, p Params) (*Response, error) {
	now := s.now()
	if r, ok := s.reports.get(p, now); ok {
		return r, nil
	}
	in, err := s.load(ctx, p, now)
	if err != nil {
		return nil, err
	}
	r := build(in, now)
	s.reports.put(p, r, now)
	return r, nil
}

// Invalidate drops cached reports, e.g. after a capcode or system edit that
// should show at once.
func (s *Service) Invalidate() { s.reports.clear() }

// StartRefresh runs an ACMA import and district assignment in the
// background. It returns false if one is already running.
func (s *Service) StartRefresh() bool {
	if !s.importing.CompareAndSwap(false, true) {
		return false
	}
	go func() {
		defer s.importing.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), importTimeout)
		defer cancel()
		s.refresh(ctx)
	}()
	return true
}

// refresh imports the register then reassigns districts. The caller holds
// the importing flag.
func (s *Service) refresh(ctx context.Context) {
	if _, _, err := importer.ImportACMA(ctx, s.db, s.opts.ACMAURL); err != nil {
		slog.Error("ACMA transmitter import failed", "error", err)
		return
	}
	if _, err := AssignDistricts(ctx, s.db); err != nil {
		slog.Error("transmitter district assignment failed", "error", err)
	}
	s.reports.clear()
}

// Run does the daily maintenance until ctx is cancelled: import the ACMA
// register when the table is empty or older than importMaxAge, otherwise
// recompute districts (new incidents and capcode edits move them), and
// prune old receptions.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTimer(maintenanceStartDelay)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.maintain(ctx)
			t.Reset(maintenanceInterval)
		}
	}
}

func (s *Service) maintain(ctx context.Context) {
	if !s.opts.ImportDisabled && s.importDue(ctx) && s.importing.CompareAndSwap(false, true) {
		importCtx, cancel := context.WithTimeout(ctx, importTimeout)
		s.refresh(importCtx)
		cancel()
		s.importing.Store(false)
	} else {
		assignCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		if _, err := AssignDistricts(assignCtx, s.db); err != nil {
			slog.Error("transmitter district assignment failed", "error", err)
		} else {
			s.reports.clear()
		}
		cancel()
	}

	if n, err := pruneReceptions(ctx, s.db, s.now()); err != nil {
		slog.Error("prune page receptions", "error", err)
	} else if n > 0 {
		slog.Info("pruned old page receptions", "deleted", n)
	}
}

// importDue reports whether the transmitter table is empty or stale.
func (s *Service) importDue(ctx context.Context) bool {
	var last sql.NullTime
	if err := s.db.GetContext(ctx, &last, `SELECT MAX(imported_at) FROM paging_transmitters`); err != nil {
		slog.Error("check ACMA import age", "error", err)
		return false
	}
	return !last.Valid || s.now().Sub(last.Time) > importMaxAge
}
