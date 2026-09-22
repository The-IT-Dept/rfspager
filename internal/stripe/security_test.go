package stripe

import (
	"context"
	"encoding/json"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	gostripe "github.com/stripe/stripe-go/v82"
	"github.com/the-it-dept/rfspager/internal/mobile"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebhookRejectsMissingSigningSecret(t *testing.T) {
	h := &Handler{}
	r := httptest.NewRequest("POST", "/webhook", strings.NewReader(`{"type":"audit.ignored","data":{"object":{}}}`))
	w := httptest.NewRecorder()
	h.Webhook(w, r)
	if w.Code != 503 {
		t.Fatalf("unsigned webhook accepted: status %d", w.Code)
	}
}
func TestCheckoutRejectsUnconfiguredPrice(t *testing.T) {
	h := &Handler{priceMonthly: "price_monthly", priceYearly: "price_yearly"}
	r := httptest.NewRequest("POST", "/checkout", strings.NewReader(`{"price_id":"price_attacker"}`))
	r = r.WithContext(context.WithValue(r.Context(), mobile.StripeUserIDKey, int64(123)))
	w := httptest.NewRecorder()
	h.CreateCheckoutSession(w, r)
	if w.Code != 400 {
		t.Fatalf("unauthenticated checkout status %d", w.Code)
	}
}

func TestStripeCancellationOnlyChangesCurrentStripeSubscription(t *testing.T) {
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	mock.ExpectExec("WHERE stripe_id = \\$1 AND subscription_platform = 'stripe' AND stripe_subscription_id = \\$2").WithArgs("cus_fixture", "sub_old").WillReturnResult(sqlmock.NewResult(0, 0))
	h := &Handler{db: sqlx.NewDb(conn, "sqlmock")}
	event := gostripe.Event{Data: &gostripe.EventData{Raw: json.RawMessage(`{"id":"sub_old","customer":"cus_fixture"}`)}}
	h.handleSubscriptionDeleted(httptest.NewRequest("POST", "/webhook", nil), event)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
