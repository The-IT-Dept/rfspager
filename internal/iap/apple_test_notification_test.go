package iap

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestAndCheckTestNotification(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	var sawPost, sawGet bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ey") {
			t.Errorf("missing bearer JWT: %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/inApps/v1/notifications/test":
			sawPost = true
			w.Write([]byte(`{"testNotificationToken":"tok-123"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/inApps/v1/notifications/test/tok-123":
			sawGet = true
			w.Write([]byte(`{"signedPayload":"x","sendAttempts":[{"attemptDate":1760000000000,"sendAttemptResult":"SUCCESS"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/inApps/v1/notifications/history":
			var body struct{ StartDate, EndDate int64 }
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.StartDate == 0 || body.EndDate <= body.StartDate {
				t.Errorf("history body: %+v err=%v", body, err)
			}
			if r.URL.Query().Get("paginationToken") == "" {
				w.Write([]byte(`{"notificationHistory":[{"signedPayload":"p1","sendAttempts":[]}],"hasMore":true,"paginationToken":"next"}`))
			} else {
				w.Write([]byte(`{"notificationHistory":[{"signedPayload":"p2","sendAttempts":[{"attemptDate":1,"sendAttemptResult":"SUCCESS"}]}],"hasMore":false}`))
			}
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"errorCode":4040008,"errorMessage":"TestNotificationNotFoundError"}`))
		}
	}))
	defer srv.Close()

	c := &AppleClient{keyID: "k", issuerID: "i", privateKey: key, bundleID: "io.devlan.rfspager", production: true, apiBase: srv.URL}
	token, err := c.RequestTestNotification("Production")
	if err != nil || token != "tok-123" {
		t.Fatalf("request: token=%q err=%v", token, err)
	}
	st, err := c.TestNotificationStatus("Production", token)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.SendAttempts) != 1 || st.SendAttempts[0].SendAttemptResult != "SUCCESS" {
		t.Errorf("unexpected status: %+v", st)
	}
	if _, err := c.TestNotificationStatus("Production", "unknown"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("want a 404 error for an unknown token, got %v", err)
	}
	if !sawPost || !sawGet {
		t.Errorf("post=%v get=%v", sawPost, sawGet)
	}
	hist, err := c.NotificationHistory("Production", time.Now().Add(-time.Hour), time.Now())
	if err != nil || len(hist) != 2 || hist[0].SignedPayload != "p1" || hist[1].SignedPayload != "p2" {
		t.Errorf("history: %+v err=%v", hist, err)
	}
	if c.base("Sandbox") != srv.URL || (&AppleClient{}).base("Sandbox") != appleSandboxURL || (&AppleClient{}).base("Production") != appleProductionURL {
		t.Errorf("base URL selection wrong")
	}
}

func TestExtendRenewalDate(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/inApps/v1/subscriptions/extend/500001" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			ExtendByDays      int    `json:"extendByDays"`
			ExtendReasonCode  int    `json:"extendReasonCode"`
			RequestIdentifier string `json:"requestIdentifier"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ExtendByDays != 90 || body.ExtendReasonCode != ExtendReasonServiceIssue || body.RequestIdentifier != "req-1" {
			t.Errorf("extend body: %+v err=%v", body, err)
		}
		w.Write([]byte(`{"effectiveDate":1806000000000,"originalTransactionId":"500001","success":true,"webOrderLineItemId":"w1"}`))
	}))
	defer srv.Close()

	c := &AppleClient{keyID: "k", issuerID: "i", privateKey: key, bundleID: "io.devlan.rfspager", production: true, apiBase: srv.URL}
	res, err := c.ExtendRenewalDate("Production", "500001", 90, ExtendReasonServiceIssue, "req-1")
	if err != nil || res.EffectiveDate != 1806000000000 || res.OriginalTransactionID != "500001" {
		t.Fatalf("extend: %+v err=%v", res, err)
	}
}
