package iap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/api/androidpublisher/v3"
	"google.golang.org/api/option"
)

func TestDeferSubscription(t *testing.T) {
	var deferred bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/applications/io.devlan.rfspager/purchases/subscriptionsv2/tokens/tok-1"):
			w.Write([]byte(`{"etag":"etag-7","subscriptionState":"SUBSCRIPTION_STATE_ACTIVE","lineItems":[{"productId":"io.devlan.rfspager.monthly","expiryTime":"2026-11-01T00:00:00Z"}]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/applications/io.devlan.rfspager/purchases/subscriptionsv2/tokens/tok-1:defer"):
			deferred = true
			var body androidpublisher.DeferSubscriptionPurchaseRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.DeferralContext == nil ||
				body.DeferralContext.Etag != "etag-7" || body.DeferralContext.DeferDuration != "2592000s" || body.DeferralContext.ValidateOnly {
				t.Errorf("defer body: %+v err=%v", body.DeferralContext, err)
			}
			w.Write([]byte(`{"itemExpiryTimeDetails":[{"productId":"io.devlan.rfspager.monthly","expiryTime":"2026-12-01T00:00:00Z"}]}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	svc, err := androidpublisher.NewService(context.Background(), option.WithEndpoint(srv.URL), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	c := &GoogleClient{service: svc, packageName: "io.devlan.rfspager"}
	got, err := c.DeferSubscription(context.Background(), "tok-1", 30)
	if err != nil {
		t.Fatal(err)
	}
	if !deferred || !got.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("deferred=%v new expiry %v", deferred, got)
	}
}
