package iap

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
)

// ExpiryGrace is how long past its expiry a subscription stays entitled
// without a store event. Expiries normally arrive as Adapty or store
// webhooks; this sweep is the safety net for ones that never do (missed
// webhooks, accounts Laravel left valid). The margin exceeds every store
// grace window (Apple billing grace up to 16 days, Google up to 30), so a
// subscriber the store is still retrying is never cut off early.
const ExpiryGrace = 30 * 24 * time.Hour

// ExpireLapsedSubscriptions switches off subscriptions whose expiry passed
// more than grace ago, recording an iap_transactions audit row for each.
// Gifted entitlements and rows with no expiry (comps) are never touched.
// Returns the user IDs it expired.
func ExpireLapsedSubscriptions(ctx context.Context, db *sqlx.DB, grace time.Duration) ([]int64, error) {
	var ids []int64
	err := db.SelectContext(ctx, &ids, `
		WITH lapsed AS (
			UPDATE users
			   SET subscription_valid = false, updated_at = NOW()
			 WHERE subscription_valid
			   AND subscription_expiry IS NOT NULL
			   AND subscription_expiry < NOW() - make_interval(secs => $1)
			   AND COALESCE(subscription_product_id, '') NOT ILIKE '%gifted%'
			RETURNING id, subscription_platform, subscription_product_id, subscription_expiry
		)
		INSERT INTO iap_transactions (user_id, platform, product_id, event_type, is_valid, expires_at, raw_payload)
		SELECT id, COALESCE(subscription_platform, 'unknown'), COALESCE(subscription_product_id, 'unknown'),
		       'expiry:sweep', false, subscription_expiry,
		       'expired by the daily sweep: no store event within the grace period'
		  FROM lapsed
		RETURNING user_id`, grace.Seconds())
	if err != nil {
		return nil, fmt.Errorf("expiring lapsed subscriptions: %w", err)
	}
	return ids, nil
}

// ExpirySweeper runs ExpireLapsedSubscriptions at start-up and then daily.
type ExpirySweeper struct {
	db       *sqlx.DB
	interval time.Duration
	stop     chan struct{}
}

func NewExpirySweeper(db *sqlx.DB, interval time.Duration) *ExpirySweeper {
	return &ExpirySweeper{db: db, interval: interval, stop: make(chan struct{})}
}

func (s *ExpirySweeper) Start() {
	go func() {
		s.run()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.run()
			case <-s.stop:
				return
			}
		}
	}()
}

func (s *ExpirySweeper) Stop() { close(s.stop) }

func (s *ExpirySweeper) run() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	ids, err := ExpireLapsedSubscriptions(ctx, s.db, ExpiryGrace)
	if err != nil {
		slog.Error("subscription expiry sweep failed", "error", err)
		return
	}
	slog.Info("subscription expiry sweep", "expired", len(ids), "user_ids", ids)
}
