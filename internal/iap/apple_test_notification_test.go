package iap

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	if c.base("Sandbox") != srv.URL || (&AppleClient{}).base("Sandbox") != appleSandboxURL || (&AppleClient{}).base("Production") != appleProductionURL {
		t.Errorf("base URL selection wrong")
	}
}
