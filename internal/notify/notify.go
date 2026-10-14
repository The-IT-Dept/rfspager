package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"

	"github.com/google/uuid"
	"github.com/the-it-dept/rfspager/internal/db"
	"github.com/the-it-dept/rfspager/internal/models"
)

var coordRegex = regexp.MustCompile(`\[(-?[0-9]+\.[0-9]+),(-?[0-9]+\.[0-9]+)\]`)

type Notifier struct {
	db  *db.DB
	fcm *FCMClient
	apn *APNClient
}

func New(database *db.DB, fcm *FCMClient, apn *APNClient) *Notifier {
	return &Notifier{db: database, fcm: fcm, apn: apn}
}

// NotifyForMessage finds all users who should be notified about this message and sends push notifications.
func (n *Notifier) NotifyForMessage(ctx context.Context, msg *models.Message) {
	userIDs := make(map[int64]bool)

	// 1. Users subscribed to this capcode
	if msg.CapcodeID.Valid {
		ids, err := n.db.GetSubscribedUsers(ctx, msg.CapcodeID.Int64)
		if err != nil {
			slog.Error("failed to get subscribed users", "error", err, "capcode_id", msg.CapcodeID.Int64)
		}
		for _, id := range ids {
			userIDs[id] = true
		}
	}

	// 2. Users with alertzones matching the message location
	if matches := coordRegex.FindStringSubmatch(msg.Message); len(matches) == 3 {
		lng, _ := strconv.ParseFloat(matches[1], 64)
		lat, _ := strconv.ParseFloat(matches[2], 64)
		ids, err := n.db.GetAlertzoneUsers(ctx, lat, lng)
		if err != nil {
			slog.Error("failed to get alertzone users", "error", err)
		}
		for _, id := range ids {
			userIDs[id] = true
		}
	}

	if len(userIDs) == 0 {
		return
	}

	// Build notification title
	title := fmt.Sprintf("Capcode %s", msg.Address)
	if msg.CapcodeID.Valid {
		capcode, err := n.db.GetCapcodeWithAgency(ctx, msg.CapcodeID.Int64)
		if err == nil && capcode != nil {
			agencyName := "Unknown"
			if capcode.AgencyName.Valid {
				agencyName = capcode.AgencyName.String
			}
			title = fmt.Sprintf("%s | %s", agencyName, capcode.Alias)
		}
	}

	// Collect unique user IDs
	ids := make([]int64, 0, len(userIDs))
	for id := range userIDs {
		ids = append(ids, id)
	}

	// Get push tokens
	targets, err := n.db.GetPushTokensForUsers(ctx, ids)
	if err != nil {
		slog.Error("failed to get push tokens", "error", err)
		return
	}

	// Group targets by user for badge counting
	userTargets := make(map[int64][]models.NotificationTarget)
	for _, t := range targets {
		userTargets[t.UserID] = append(userTargets[t.UserID], t)
	}

	// Build notification data for database storage
	notifData, _ := json.Marshal(map[string]any{
		"id":         msg.ID,
		"system_id":  msg.SystemID,
		"capcode_id": msg.CapcodeID,
		"address":    msg.Address,
		"message":    msg.Message,
		"created_at": msg.CreatedAt,
	})

	for userID, userTokens := range userTargets {
		// Store in database notification table (Laravel format)
		notifID := uuid.New().String()
		if err := n.db.InsertDatabaseNotification(ctx, notifID,
			userID, `App\Notifications\PagerNotification`, notifData); err != nil {
			slog.Error("failed to insert db notification", "error", err, "user_id", userID)
		}

		// Get badge count
		count, err := n.db.GetUnreadNotificationCount(ctx, userID)
		if err != nil {
			slog.Error("failed to get unread count", "error", err, "user_id", userID)
			count = 1
		}

		for _, target := range userTokens {
			switch target.PushType {
			case "GCM":
				if n.fcm != nil {
					if err := n.fcm.Send(ctx, target.PushToken, title, msg.Message, count); err != nil {
						slog.Error("FCM send failed", "error", err, "user_id", userID)
					}
				}
			case "APN":
				if n.apn != nil {
					if err := n.apn.Send(ctx, target.PushToken, title, msg.Message, count); err != nil {
						slog.Error("APN send failed", "error", err, "user_id", userID)
					}
				}
			}
		}
	}

	slog.Info("notifications sent", "message_id", msg.ID, "user_count", len(userTargets))
}

// SendDirect pushes a plain title+body notification to a single user, on
// every push target they have. Used for transactional events like "system
// went offline" — extra map values are merged into the push data so the
// client can deep-link to the right screen.
//
// Best-effort: errors are logged but not propagated.
func (n *Notifier) SendDirect(ctx context.Context, userID int64, title, body string, _ map[string]string) {
	targets, err := n.db.GetPushTokensForUsers(ctx, []int64{userID})
	if err != nil {
		slog.Error("send-direct: get push tokens failed", "user_id", userID, "err", err)
		return
	}
	for _, target := range targets {
		switch target.PushType {
		case "GCM":
			if n.fcm != nil {
				if err := n.fcm.Send(ctx, target.PushToken, title, body, 0); err != nil {
					slog.Error("FCM send failed (direct)", "user_id", userID, "err", err)
				}
			}
		case "APN":
			if n.apn != nil {
				if err := n.apn.Send(ctx, target.PushToken, title, body, 0); err != nil {
					slog.Error("APN send failed (direct)", "user_id", userID, "err", err)
				}
			}
		}
	}
}

// NotifySystemApproved pushes a one-off notification to the owner when their
// pending system request is approved. Best-effort: errors are logged, not
// surfaced to the admin who clicked Approve.
func (n *Notifier) NotifySystemApproved(ctx context.Context, userID int64, systemName string) {
	targets, err := n.db.GetPushTokensForUsers(ctx, []int64{userID})
	if err != nil {
		slog.Error("failed to get push tokens for system approval", "error", err, "user_id", userID)
		return
	}
	if len(targets) == 0 {
		slog.Info("no push tokens for approved system owner", "user_id", userID)
		return
	}

	title := "System approved"
	body := fmt.Sprintf("\"%s\" is ready. Open RFSPager to grab your install command.", systemName)

	for _, target := range targets {
		switch target.PushType {
		case "GCM":
			if n.fcm != nil {
				if err := n.fcm.Send(ctx, target.PushToken, title, body, 0); err != nil {
					slog.Error("FCM send failed (system approval)", "error", err, "user_id", userID)
				}
			}
		case "APN":
			if n.apn != nil {
				if err := n.apn.Send(ctx, target.PushToken, title, body, 0); err != nil {
					slog.Error("APN send failed (system approval)", "error", err, "user_id", userID)
				}
			}
		}
	}
}

// NotifyHostCompStarted tells a receiver host they have Pro free while their
// receiver stays online. Best-effort, like NotifySystemApproved.
func (n *Notifier) NotifyHostCompStarted(ctx context.Context, userID int64, systemName string) {
	title, body := HostCompStartedPush(systemName)
	n.pushToUser(ctx, userID, title, body, "host comp started")
}

// NotifyHostCompLapsed tells a receiver host their free Pro has paused
// because the receiver has not been online enough. Best-effort.
func (n *Notifier) NotifyHostCompLapsed(ctx context.Context, userID int64, systemName string) {
	title, body := HostCompLapsedPush(systemName)
	n.pushToUser(ctx, userID, title, body, "host comp lapsed")
}

// HostCompStartedPush is the start push copy.
func HostCompStartedPush(systemName string) (title, body string) {
	return "Pro is on us", fmt.Sprintf("Thanks for hosting %s. You've got RFSPager Pro free while it stays online.", receiverLabel(systemName))
}

// HostCompLapsedPush is the lapse push copy.
func HostCompLapsedPush(systemName string) (title, body string) {
	return "Free Pro paused", fmt.Sprintf("%s hasn't been online enough lately. Pro comes back once it's been steadily online for about three days.", upperFirst(receiverLabel(systemName)))
}

func receiverLabel(systemName string) string {
	if systemName == "" {
		return "your receiver"
	}
	return `"` + systemName + `"`
}

func upperFirst(s string) string {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return s
	}
	return string(s[0]-'a'+'A') + s[1:]
}

// pushToUser sends title/body to every push target the user has.
func (n *Notifier) pushToUser(ctx context.Context, userID int64, title, body, what string) {
	if n == nil || n.db == nil {
		return
	}
	targets, err := n.db.GetPushTokensForUsers(ctx, []int64{userID})
	if err != nil {
		slog.Error("failed to get push tokens", "what", what, "error", err, "user_id", userID)
		return
	}
	for _, target := range targets {
		switch target.PushType {
		case "GCM":
			if n.fcm != nil {
				if err := n.fcm.Send(ctx, target.PushToken, title, body, 0); err != nil {
					slog.Error("FCM send failed", "what", what, "error", err, "user_id", userID)
				}
			}
		case "APN":
			if n.apn != nil {
				if err := n.apn.Send(ctx, target.PushToken, title, body, 0); err != nil {
					slog.Error("APN send failed", "what", what, "error", err, "user_id", userID)
				}
			}
		}
	}
}
