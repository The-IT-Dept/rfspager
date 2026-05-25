package iap

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/api/androidpublisher/v3"
	"google.golang.org/api/option"
)

// GoogleClient validates purchases against Google Play Developer API.
type GoogleClient struct {
	service     *androidpublisher.Service
	packageName string
}

func NewGoogleClient(ctx context.Context, credentialsFile, packageName string) (*GoogleClient, error) {
	if credentialsFile == "" || packageName == "" {
		slog.Warn("Google Play IAP credentials not configured")
		return nil, nil
	}

	svc, err := androidpublisher.NewService(ctx, option.WithCredentialsFile(credentialsFile))
	if err != nil {
		return nil, fmt.Errorf("creating androidpublisher service: %w", err)
	}

	return &GoogleClient{
		service:     svc,
		packageName: packageName,
	}, nil
}

// ValidatePurchase validates a subscription purchase token with Google Play.
func (c *GoogleClient) ValidatePurchase(ctx context.Context, purchaseToken, productID string) (*ValidationResult, error) {
	// Use Subscriptions v2 API
	result, err := c.service.Purchases.Subscriptionsv2.Get(c.packageName, purchaseToken).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("Google Play API error: %w", err)
	}

	// Parse expiry from line items
	var expiresAt *time.Time
	var willRenew bool
	resolvedProductID := productID

	if len(result.LineItems) > 0 {
		item := result.LineItems[0]
		if item.ExpiryTime != "" {
			t, err := time.Parse(time.RFC3339, item.ExpiryTime)
			if err == nil {
				expiresAt = &t
			}
		}
		if item.AutoRenewingPlan != nil {
			willRenew = item.AutoRenewingPlan.AutoRenewEnabled
		}
		if item.OfferDetails != nil && len(item.OfferDetails.BasePlanId) > 0 {
			resolvedProductID = item.OfferDetails.BasePlanId
		}
	}

	// Check subscription state
	// SUBSCRIPTION_STATE_ACTIVE = active
	// SUBSCRIPTION_STATE_EXPIRED = expired
	// SUBSCRIPTION_STATE_REVOKED = revoked
	// SUBSCRIPTION_STATE_ON_HOLD = on hold (payment issue)
	// SUBSCRIPTION_STATE_IN_GRACE_PERIOD = grace period
	valid := result.SubscriptionState == "SUBSCRIPTION_STATE_ACTIVE" ||
		result.SubscriptionState == "SUBSCRIPTION_STATE_IN_GRACE_PERIOD"

	return &ValidationResult{
		Valid:                 valid,
		ProductID:             resolvedProductID,
		Platform:              "google",
		ExpiresAt:             expiresAt,
		OriginalTransactionID: purchaseToken,
		WillRenew:             willRenew,
	}, nil
}
