package recvhub

import (
	"context"
	"log/slog"
	"time"
)

// logRetention is how long we keep receiver log lines. Matches the policy
// chosen in the product brief — full persistence with daily rotation.
const logRetention = 7 * 24 * time.Hour

// runLogRotation drops log rows older than logRetention. Runs once on a
// 1-hour cycle (cheap; partial scan via idx_system_logs_received_at).
func (h *Hub) runLogRotation(ctx context.Context) {
	// First run: 5 minutes after boot so we don't fight migrations or the
	// initial connection storm.
	t := time.NewTimer(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := h.Logs.RotateOlderThan(ctx, logRetention); err != nil {
				slog.Warn("log rotation failed", "err", err)
			} else if n > 0 {
				slog.Info("rotated old system logs", "deleted", n)
			}
			t.Reset(1 * time.Hour)
		}
	}
}
