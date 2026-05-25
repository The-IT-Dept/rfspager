package notify

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"

	"github.com/sideshow/apns2"
	"github.com/sideshow/apns2/payload"
	"github.com/sideshow/apns2/token"
)

type APNClient struct {
	client   *apns2.Client
	bundleID string
}

func NewAPN(keyID, teamID, bundleID, privateKeyPEM string, production bool) (*APNClient, error) {
	if keyID == "" || teamID == "" || privateKeyPEM == "" {
		slog.Warn("APN credentials not configured, APN notifications disabled")
		return nil, nil
	}

	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("failed to decode APN private key PEM")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		// Try EC private key format
		key, err = x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parsing APN private key: %w", err)
		}
	}

	ecdsaKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("APN private key is not ECDSA")
	}

	authToken := &token.Token{
		AuthKey: ecdsaKey,
		KeyID:   keyID,
		TeamID:  teamID,
	}

	var client *apns2.Client
	if production {
		client = apns2.NewTokenClient(authToken).Production()
	} else {
		client = apns2.NewTokenClient(authToken).Development()
	}

	return &APNClient{client: client, bundleID: bundleID}, nil
}

func (a *APNClient) Send(ctx context.Context, deviceToken, title, body string, badge int) error {
	p := payload.NewPayload().
		AlertTitle(title).
		AlertBody(body).
		Badge(badge).
		Sound("default")

	notification := &apns2.Notification{
		DeviceToken: deviceToken,
		Topic:       a.bundleID,
		Payload:     p,
	}

	resp, err := a.client.PushWithContext(ctx, notification)
	if err != nil {
		return fmt.Errorf("sending APN notification: %w", err)
	}

	if !resp.Sent() {
		return fmt.Errorf("APN notification not sent: %s (status %d)", resp.Reason, resp.StatusCode)
	}

	return nil
}
