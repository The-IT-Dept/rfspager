package notify

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"

	"github.com/the-it-dept/rfspager/internal/models"
)

// ClaimedNotification is one row pulled off the dispatch queue. The data field
// is the JSON payload the fan-out worker stored on the notifications row — it
// must include title and body so we don't re-query.
type ClaimedNotification struct {
	ID       string // notifications.id (uuid)
	UserID   int64  // notifications.notifiable_id
	Data     []byte // notifications.data (JSON)
	Attempts int    // notifications.push_attempts (post-claim)
}

// DispatchOutcome reports what happened for a single ClaimedNotification.
type DispatchOutcome struct {
	ID            string
	Sent          bool   // at least one device succeeded
	PermanentFail bool   // every device returned a non-retryable error
	Err           string // joined error string for logging / push_last_error column
	BadTokens     []string
}

// SendBatch ships a batch of already-claimed notifications. It fans out to
// APN/FCM per device, returns per-row outcomes so the caller can mark
// pushed_at / push_failed_at on each row.
//
// The caller has already incremented push_attempts and set a lease on each
// row — SendBatch does no DB writes of its own.
func (n *Notifier) SendBatch(ctx context.Context, batch []ClaimedNotification) []DispatchOutcome {
	if len(batch) == 0 {
		return nil
	}

	// Collect unique user IDs, then load push tokens and unread counts in one go.
	userIDs := make([]int64, 0, len(batch))
	seen := make(map[int64]bool)
	for _, c := range batch {
		if !seen[c.UserID] {
			seen[c.UserID] = true
			userIDs = append(userIDs, c.UserID)
		}
	}

	targets, err := n.db.GetPushTokensForUsers(ctx, userIDs)
	if err != nil {
		slog.Error("dispatch: failed to load push tokens", "error", err)
		// Treat every row as transient-failed so the lease expires and we retry.
		out := make([]DispatchOutcome, len(batch))
		for i, c := range batch {
			out[i] = DispatchOutcome{ID: c.ID, Err: "push token lookup failed"}
		}
		return out
	}

	tokensByUser := make(map[int64][]models.NotificationTarget, len(userIDs))
	for _, t := range targets {
		tokensByUser[t.UserID] = append(tokensByUser[t.UserID], t)
	}

	// Unread counts (used for badge). Best-effort — fail to 1 on error.
	badgeByUser := make(map[int64]int, len(userIDs))
	for _, uid := range userIDs {
		c, err := n.db.GetUnreadNotificationCount(ctx, uid)
		if err != nil {
			c = 1
		}
		badgeByUser[uid] = c
	}

	outcomes := make([]DispatchOutcome, len(batch))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 32) // bound parallelism per batch

	for i, claim := range batch {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, c ClaimedNotification) {
			defer wg.Done()
			defer func() { <-sem }()
			outcomes[i] = n.sendOne(ctx, c, tokensByUser[c.UserID], badgeByUser[c.UserID])
		}(i, claim)
	}

	wg.Wait()
	return outcomes
}

// sendOne ships a single claim to every push target for the user.
func (n *Notifier) sendOne(ctx context.Context, claim ClaimedNotification, targets []models.NotificationTarget, badge int) DispatchOutcome {
	out := DispatchOutcome{ID: claim.ID}

	if len(targets) == 0 {
		// User has no devices — permanent fail, no point retrying.
		out.PermanentFail = true
		out.Err = "no push tokens"
		return out
	}

	var payload struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal(claim.Data, &payload); err != nil {
		out.PermanentFail = true
		out.Err = "invalid payload JSON: " + err.Error()
		return out
	}
	if payload.Title == "" {
		payload.Title = "RFS Pager"
	}

	var (
		errs           []string
		nonRetryable   = 0
		retryable      = 0
		successes      = 0
		badTokens      []string
	)
	for _, t := range targets {
		var err error
		switch t.PushType {
		case "APN":
			if n.apn == nil {
				continue
			}
			err = n.apn.Send(ctx, t.PushToken, payload.Title, payload.Body, badge)
		case "GCM":
			if n.fcm == nil {
				continue
			}
			err = n.fcm.Send(ctx, t.PushToken, payload.Title, payload.Body, badge)
		default:
			continue
		}
		if err == nil {
			successes++
			continue
		}
		errs = append(errs, t.PushType+": "+err.Error())
		if isBadTokenError(err) {
			nonRetryable++
			badTokens = append(badTokens, t.PushToken)
		} else {
			retryable++
		}
	}

	out.BadTokens = badTokens
	if successes > 0 {
		out.Sent = true
		if len(errs) > 0 {
			out.Err = joinErrs(errs)
		}
		return out
	}
	if retryable == 0 && nonRetryable > 0 {
		out.PermanentFail = true
	}
	out.Err = joinErrs(errs)
	return out
}

func joinErrs(errs []string) string {
	if len(errs) == 0 {
		return ""
	}
	return errors.Join(toErrors(errs)...).Error()
}

func toErrors(s []string) []error {
	out := make([]error, len(s))
	for i, v := range s {
		out[i] = errors.New(v)
	}
	return out
}

// isBadTokenError detects provider-returned "device token is gone" responses.
// Conservative — anything we don't recognize is treated as transient.
func isBadTokenError(err error) bool {
	msg := err.Error()
	// APN reasons (from sideshow/apns2) — non-recoverable on the token.
	for _, s := range []string{
		"BadDeviceToken",
		"Unregistered",
		"DeviceTokenNotForTopic",
	} {
		if contains(msg, s) {
			return true
		}
	}
	// FCM error codes — non-recoverable on the token.
	for _, s := range []string{
		"registration-token-not-registered",
		"invalid-argument",
		"invalid-registration-token",
	} {
		if contains(msg, s) {
			return true
		}
	}
	return false
}

func contains(haystack, needle string) bool {
	if len(needle) == 0 || len(haystack) < len(needle) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
