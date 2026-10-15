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

	return googleValidationResult(result, purchaseToken, productID)
}

// DeferSubscription pushes a subscription's next renewal back by days, so
// Google doesn't charge until the free time runs out, and returns the new
// expiry Google reports. The deferral is pinned to the etag just read, so it
// fails rather than stacking on a change it hasn't seen. Google then sends a
// SUBSCRIPTION_DEFERRED notification, which the webhook applies by
// re-validating the purchase.
func (c *GoogleClient) DeferSubscription(ctx context.Context, purchaseToken string, days int) (time.Time, error) {
	current, err := c.service.Purchases.Subscriptionsv2.Get(c.packageName, purchaseToken).Context(ctx).Do()
	if err != nil {
		return time.Time{}, fmt.Errorf("Google Play API error: %w", err)
	}
	res, err := c.service.Purchases.Subscriptionsv2.Defer(c.packageName, purchaseToken, &androidpublisher.DeferSubscriptionPurchaseRequest{
		DeferralContext: &androidpublisher.DeferralContext{
			DeferDuration: fmt.Sprintf("%ds", days*24*60*60),
			Etag:          current.Etag,
		},
	}).Context(ctx).Do()
	if err != nil {
		return time.Time{}, fmt.Errorf("Google Play API error: %w", err)
	}
	var latest time.Time
	for _, item := range res.ItemExpiryTimeDetails {
		if item == nil || item.ExpiryTime == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, item.ExpiryTime)
		if err != nil {
			return time.Time{}, fmt.Errorf("parsing Google Play expiry: %w", err)
		}
		if t.After(latest) {
			latest = t
		}
	}
	if latest.IsZero() {
		return time.Time{}, fmt.Errorf("Google Play deferral returned no expiry")
	}
	return latest, nil
}

func googleValidationResult(purchase *androidpublisher.SubscriptionPurchaseV2, purchaseToken, requestedProductID string) (*ValidationResult, error) {
	if len(purchase.LineItems) == 0 || purchase.LineItems[0] == nil || purchase.LineItems[0].ProductId == "" {
		return nil, fmt.Errorf("Google Play response contains no subscription product")
	}
	item := purchase.LineItems[0]
	for _, other := range purchase.LineItems[1:] {
		if other == nil || other.ProductId != item.ProductId {
			return nil, fmt.Errorf("Google Play response contains conflicting subscription products")
		}
	}
	if requestedProductID != "" && requestedProductID != item.ProductId {
		return nil, fmt.Errorf("requested product %q does not match validated product %q", requestedProductID, item.ProductId)
	}

	var expiresAt *time.Time
	if item.ExpiryTime != "" {
		t, err := time.Parse(time.RFC3339, item.ExpiryTime)
		if err != nil {
			return nil, fmt.Errorf("parsing Google Play expiry: %w", err)
		}
		expiresAt = &t
	}
	willRenew := item.AutoRenewingPlan != nil && item.AutoRenewingPlan.AutoRenewEnabled
	var valid bool
	switch purchase.SubscriptionState {
	case "SUBSCRIPTION_STATE_ACTIVE", "SUBSCRIPTION_STATE_IN_GRACE_PERIOD":
		valid = true
	case "SUBSCRIPTION_STATE_CANCELED":
		// Cancelling only stops renewal: Google keeps the subscriber entitled
		// until the paid period ends, so access must too.
		valid = expiresAt != nil && expiresAt.After(time.Now())
	default:
		// ON_HOLD (payment failed past grace), PAUSED, EXPIRED and PENDING
		// carry no entitlement.
		valid = false
	}
	environment := "Production"
	if purchase.TestPurchase != nil {
		environment = "Test"
	}
	transactionID := item.LatestSuccessfulOrderId
	if transactionID == "" {
		transactionID = purchase.LatestOrderId
	}

	return &ValidationResult{
		Valid:                 valid,
		ProductID:             item.ProductId,
		Platform:              "google",
		Environment:           environment,
		ExpiresAt:             expiresAt,
		TransactionID:         transactionID,
		OriginalTransactionID: purchaseToken,
		PreviousOwnershipID:   purchase.LinkedPurchaseToken,
		WillRenew:             willRenew,
	}, nil
}
