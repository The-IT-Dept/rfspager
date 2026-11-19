package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"
)

type FCMClient struct {
	client *messaging.Client
}

func NewFCM(ctx context.Context, credentialsFile string) (*FCMClient, error) {
	if credentialsFile == "" {
		slog.Warn("FCM credentials not configured, FCM notifications disabled")
		return nil, nil
	}

	app, err := firebase.NewApp(ctx, nil, option.WithCredentialsFile(credentialsFile))
	if err != nil {
		return nil, fmt.Errorf("initializing firebase app: %w", err)
	}

	client, err := app.Messaging(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting firebase messaging client: %w", err)
	}

	return &FCMClient{client: client}, nil
}

func (f *FCMClient) Send(ctx context.Context, token, title, body string, badge int) error {
	return f.SendWith(ctx, token, title, body, badge, PushOptions{})
}

// SendWith is Send with per-push options. The collapse id becomes the
// Android notification tag, so a later push about the same page replaces
// this one in the tray (not the FCM collapse_key: FCM keeps only four of
// those per offline device and would drop pages), and travels in the data
// as "collapse" for the app's own foreground display. A correction goes on
// the app's quiet "corrections" channel (FCM falls back to the default
// channel on app versions that don't create it) and is delivered now or
// never, so an offline phone just gets the original page.
func (f *FCMClient) SendWith(ctx context.Context, token, title, body string, badge int, opts PushOptions) error {
	notification := &messaging.AndroidNotification{
		Color:             "#800000", // Laravel sent "#80000"; FCM requires #RRGGBB
		Tag:               opts.CollapseID,
		NotificationCount: intPtr(badge),
	}
	android := &messaging.AndroidConfig{Notification: notification}
	if opts.CollapseID != "" {
		android.Data = map[string]string{"collapse": opts.CollapseID}
	}
	if opts.Correction {
		notification.ChannelID = CorrectionsChannel
		zero := time.Duration(0)
		android.TTL = &zero
		if android.Data == nil {
			android.Data = map[string]string{}
		}
		android.Data["correction"] = "1"
	} else {
		notification.Sound = "default"
	}
	msg := &messaging.Message{
		Token: token,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
		Android: android,
	}

	_, err := f.client.Send(ctx, msg)
	if err != nil {
		// Classify before wrapping: the SDK's Is* helpers type-assert rather
		// than unwrap, so they cannot see the error once it is wrapped.
		if messaging.IsUnregistered(err) || messaging.IsSenderIDMismatch(err) {
			return fmt.Errorf("sending FCM message: %w: %w", ErrBadToken, err)
		}
		return fmt.Errorf("sending FCM message: %w", err)
	}
	return nil
}

// PushOptions are per-push extras on top of title, body and badge.
type PushOptions struct {
	// CollapseID names the page a push is about. A later push with the same
	// id replaces the earlier notification on the device (APNs
	// apns-collapse-id, Android notification tag).
	CollapseID string
	// Correction marks a push that replaces an earlier one about the same
	// page with cleaner text: no sound (Android 8+ via the quiet
	// CorrectionsChannel), and delivered now or never. APNs stores only the
	// latest push per app for an offline device, so a stored correction
	// could displace a newer page.
	Correction bool
}

// CorrectionsChannel is the Android notification channel the app creates
// for correction pushes, at low importance with no sound.
const CorrectionsChannel = "corrections"

// ErrBadToken marks a push token the provider says will never work again
// (app uninstalled, token rotated). The dispatcher retires such tokens
// instead of retrying them.
var ErrBadToken = errors.New("push token is no longer registered")

func intPtr(i int) *int {
	return &i
}
