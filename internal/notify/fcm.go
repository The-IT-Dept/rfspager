package notify

import (
	"context"
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
		return fmt.Errorf("sending FCM message: %w", err)
	}
	return nil
}

func intPtr(i int) *int {
	return &i
}
