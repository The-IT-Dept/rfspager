package stripe

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/mobile"
	gostripe "github.com/stripe/stripe-go/v82"
	portalsession "github.com/stripe/stripe-go/v82/billingportal/session"
	checkoutsession "github.com/stripe/stripe-go/v82/checkout/session"
	"github.com/stripe/stripe-go/v82/customer"
	"github.com/stripe/stripe-go/v82/webhook"
)

type Handler struct {
	db             *sqlx.DB
	priceMonthly   string
	priceYearly    string
	webhookSecret  string
	baseURL        string
}

func NewHandler(db *sqlx.DB, secretKey, priceMonthly, priceYearly, webhookSecret, baseURL string) *Handler {
	if secretKey == "" {
		slog.Warn("Stripe not configured")
		return nil
	}
	gostripe.Key = secretKey
	return &Handler{
		db:            db,
		priceMonthly:  priceMonthly,
		priceYearly:   priceYearly,
		webhookSecret: webhookSecret,
		baseURL:       baseURL,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// CreateCheckoutSession creates a Stripe Checkout session for subscription.
func (h *Handler) CreateCheckoutSession(w http.ResponseWriter, r *http.Request) {
	userID, email := getUserFromContext(r)
	if userID == 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req struct {
		PriceID string `json:"price_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	if req.PriceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "price_id required"})
		return
	}

	// Get or create Stripe customer
	customerID := h.getOrCreateCustomer(r, userID, email)
	if customerID == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create customer"})
		return
	}

	params := &gostripe.CheckoutSessionParams{
		Customer: gostripe.String(customerID),
		Mode:     gostripe.String(string(gostripe.CheckoutSessionModeSubscription)),
		LineItems: []*gostripe.CheckoutSessionLineItemParams{
			{Price: gostripe.String(req.PriceID), Quantity: gostripe.Int64(1)},
		},
		SuccessURL: gostripe.String(h.baseURL + "/subscribe?success=true"),
		CancelURL:  gostripe.String(h.baseURL + "/subscribe?canceled=true"),
	}

	s, err := checkoutsession.New(params)
	if err != nil {
		slog.Error("stripe checkout session failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create session"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"url": s.URL})
}

// CreatePortalSession creates a Stripe Customer Portal session for managing subscriptions.
func (h *Handler) CreatePortalSession(w http.ResponseWriter, r *http.Request) {
	userID, _ := getUserFromContext(r)
	if userID == 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var customerID string
	h.db.GetContext(r.Context(), &customerID, `SELECT COALESCE(stripe_id, '') FROM users WHERE id = $1`, userID)
	if customerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no subscription found"})
		return
	}

	params := &gostripe.BillingPortalSessionParams{
		Customer:  gostripe.String(customerID),
		ReturnURL: gostripe.String(h.baseURL + "/subscribe"),
	}

	s, err := portalsession.New(params)
	if err != nil {
		slog.Error("stripe portal session failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create portal session"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"url": s.URL})
}

// Prices returns available subscription prices.
func (h *Handler) Prices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"monthly": h.priceMonthly,
		"yearly":  h.priceYearly,
	})
}

// Webhook handles Stripe webhook events.
func (h *Handler) Webhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var event gostripe.Event
	if h.webhookSecret != "" {
		event, err = webhook.ConstructEvent(body, r.Header.Get("Stripe-Signature"), h.webhookSecret)
		if err != nil {
			slog.Error("stripe webhook signature failed", "error", err)
			http.Error(w, "invalid signature", http.StatusBadRequest)
			return
		}
	} else {
		json.Unmarshal(body, &event)
	}

	slog.Info("stripe webhook", "type", event.Type)

	switch event.Type {
	case "customer.subscription.created", "customer.subscription.updated":
		h.handleSubscriptionUpdate(r, event)
	case "customer.subscription.deleted":
		h.handleSubscriptionDeleted(r, event)
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleSubscriptionUpdate(r *http.Request, event gostripe.Event) {
	var sub gostripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		slog.Error("stripe webhook: unmarshal sub failed", "error", err)
		return
	}

	customerID := sub.Customer.ID
	valid := sub.Status == gostripe.SubscriptionStatusActive || sub.Status == gostripe.SubscriptionStatusTrialing
	willRenew := !sub.CancelAtPeriodEnd

	productID := ""
	var expiresAt *time.Time
	var startDate *time.Time
	if len(sub.Items.Data) > 0 {
		item := sub.Items.Data[0]
		productID = item.Price.ID
		if item.CurrentPeriodEnd > 0 {
			t := time.Unix(item.CurrentPeriodEnd, 0)
			expiresAt = &t
		}
		if item.CurrentPeriodStart > 0 {
			t := time.Unix(item.CurrentPeriodStart, 0)
			startDate = &t
		}
	}

	_, err := h.db.ExecContext(r.Context(),
		`UPDATE users SET
			subscription_valid = $1,
			subscription_expiry = $2,
			subscription_product_id = $3,
			subscription_platform = 'stripe',
			will_renew = $4,
			stripe_subscription_id = $5,
			stripe_subscription_status = $6,
			stripe_subscription_start_date = $7,
			stripe_subscription_end_date = $8,
			last_store_sync_at = NOW(),
			updated_at = NOW()
		 WHERE stripe_id = $9`,
		valid, expiresAt, productID, willRenew,
		sub.ID, string(sub.Status),
		startDate, expiresAt,
		customerID)
	if err != nil {
		slog.Error("stripe webhook: update user failed", "error", err, "customer", customerID)
	} else {
		slog.Info("stripe webhook: subscription updated", "customer", customerID, "status", sub.Status, "valid", valid)
	}
}

func (h *Handler) handleSubscriptionDeleted(r *http.Request, event gostripe.Event) {
	var sub gostripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		return
	}

	_, err := h.db.ExecContext(r.Context(),
		`UPDATE users SET
			subscription_valid = false,
			will_renew = false,
			stripe_subscription_status = 'canceled',
			cancellation_reason = 'stripe_canceled',
			last_store_sync_at = NOW(),
			updated_at = NOW()
		 WHERE stripe_id = $1`, sub.Customer.ID)
	if err != nil {
		slog.Error("stripe webhook: cancel failed", "error", err)
	}
}

func (h *Handler) getOrCreateCustomer(r *http.Request, userID int64, email string) string {
	var existing string
	h.db.GetContext(r.Context(), &existing, `SELECT COALESCE(stripe_id, '') FROM users WHERE id = $1`, userID)
	if existing != "" {
		return existing
	}

	params := &gostripe.CustomerParams{
		Email: gostripe.String(email),
	}
	c, err := customer.New(params)
	if err != nil {
		slog.Error("stripe create customer failed", "error", err)
		return ""
	}

	h.db.ExecContext(r.Context(), `UPDATE users SET stripe_id = $1 WHERE id = $2`, c.ID, userID)
	return c.ID
}

func getUserFromContext(r *http.Request) (int64, string) {
	id, _ := r.Context().Value(mobile.StripeUserIDKey).(int64)
	email, _ := r.Context().Value(mobile.StripeUserEmailKey).(string)
	return id, email
}
