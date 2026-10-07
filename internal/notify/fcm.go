package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

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
	msg := &messaging.Message{
		Token: token,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
		Android: &messaging.AndroidConfig{
			Notification: &messaging.AndroidNotification{
				Color:             "#800000", // Laravel sent "#80000"; FCM requires #RRGGBB
				Sound:             "default",
				NotificationCount: intPtr(badge),
			},
		},
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

// ErrBadToken marks a push token the provider says will never work again
// (app uninstalled, token rotated). The dispatcher retires such tokens
// instead of retrying them.
var ErrBadToken = errors.New("push token is no longer registered")

func intPtr(i int) *int {
	return &i
}
