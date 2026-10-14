// apple-notify-test exercises App Store Server Notifications end to end.
//
// Run it on apps01 from /opt/rfspager-go so it reads the server's .env:
//
//	./apple-notify-test                      request a TEST notification, report Apple's delivery attempts
//	./apple-notify-test -history 48h         list what Apple sent in that window and run every signed
//	                                         payload through the webhook's own verification (no DB)
//	./apple-notify-test -history 48h -replay https://go.dev.blah.au/api/webhooks/apple
//	                                         additionally POST each payload to that webhook, as Apple would
//
// -env selects Production (default) or Sandbox.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/the-it-dept/rfspager/internal/iap"
)

func main() {
	env := flag.String("env", "Production", "Production or Sandbox")
	history := flag.Duration("history", 0, "list and verify notifications Apple produced in this window (e.g. 48h)")
	replay := flag.String("replay", "", "with -history: POST every signed payload to this webhook URL")
	flag.Parse()
	_ = godotenv.Load()
	bundle := os.Getenv("APPLE_BUNDLE_ID")
	if bundle == "" {
		bundle = "io.devlan.rfspager"
	}
	client, err := iap.NewAppleClient(os.Getenv("APPLE_KEY_ID"), os.Getenv("APPLE_ISSUER_ID"), os.Getenv("APPLE_PRIVATE_KEY"), bundle, *env == "Production")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if client == nil {
		fmt.Fprintln(os.Stderr, "APPLE_KEY_ID, APPLE_ISSUER_ID and APPLE_PRIVATE_KEY must be set")
		os.Exit(1)
	}
	if *history > 0 {
		os.Exit(runHistory(client, *env, *history, *replay))
	}
	os.Exit(runTest(client, *env))
}

func runTest(client *iap.AppleClient, env string) int {
	token, err := client.RequestTestNotification(env)
	if err != nil {
		fmt.Fprintln(os.Stderr, "request failed:", err)
		return 1
	}
	fmt.Printf("%s: test notification requested, token %s\n", env, token)
	for i := 0; i < 18; i++ {
		time.Sleep(5 * time.Second)
		st, err := client.TestNotificationStatus(env, token)
		if err != nil {
			if strings.Contains(err.Error(), "404") {
				fmt.Println("  not processed yet")
				continue
			}
			fmt.Fprintln(os.Stderr, "status failed:", err)
			return 1
		}
		if len(st.SendAttempts) == 0 {
			fmt.Println("  no delivery attempt yet")
			continue
		}
		for _, a := range st.SendAttempts {
			fmt.Printf("  %s  %s\n", time.UnixMilli(a.AttemptDate).UTC().Format(time.RFC3339), a.SendAttemptResult)
		}
		return 0
	}
	fmt.Println("gave up waiting; query again later with the token above")
	return 2
}

// unverifiedClaims reads display-only fields (environment, bundle) from a JWS
// without checking it; verification is done separately by the iap package.
func unverifiedClaims(jws string) map[string]any {
	parts := strings.Split(jws, ".")
	if len(parts) != 3 {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return m
}

func runHistory(client *iap.AppleClient, env string, window time.Duration, replayURL string) int {
	end := time.Now()
	items, err := client.NotificationHistory(env, end.Add(-window), end)
	if err != nil {
		fmt.Fprintln(os.Stderr, "history failed:", err)
		return 1
	}
	fmt.Printf("%s: %d notifications in the last %s\n", env, len(items), window)
	verifyFailures, replayFailures := 0, 0
	for _, it := range items {
		notif, err := iap.DecodeAppleNotification(it.SignedPayload)
		if err != nil {
			verifyFailures++
			fmt.Printf("  VERIFY FAILED (envelope): %v\n", err)
			continue
		}
		line := fmt.Sprintf("  %-28s %-22s", notif.NotificationType, notif.Subtype)
		if claims := unverifiedClaims(it.SignedPayload); claims != nil {
			if data, ok := claims["data"].(map[string]any); ok {
				line += fmt.Sprintf(" %-10v", data["environment"])
			}
		}
		if notif.Data.SignedTransactionInfo != "" {
			txn, err := iap.DecodeAppleJWS(notif.Data.SignedTransactionInfo)
			if err != nil {
				verifyFailures++
				line += fmt.Sprintf("  TRANSACTION VERIFY FAILED: %v", err)
			} else {
				line += fmt.Sprintf("  %s expires %s otx=%s exp_ms=%d", txn.ProductID, time.UnixMilli(txn.ExpiresDate).UTC().Format("2006-01-02"), txn.OriginalTransactionID, txn.ExpiresDate)
			}
			if notif.Data.SignedRenewalInfo != "" {
				if _, err := iap.DecodeRenewalInfo(notif.Data.SignedRenewalInfo); err != nil {
					verifyFailures++
					line += fmt.Sprintf("  RENEWAL VERIFY FAILED: %v", err)
				}
			}
		} else {
			line += "  (no transaction)"
		}
		for _, a := range it.SendAttempts {
			line += fmt.Sprintf("  [%s %s]", time.UnixMilli(a.AttemptDate).UTC().Format("01-02 15:04"), a.SendAttemptResult)
		}
		if replayURL != "" {
			code, err := post(replayURL, it.SignedPayload)
			if err != nil || code != http.StatusOK {
				replayFailures++
				line += fmt.Sprintf("  REPLAY -> %d %v", code, err)
			} else {
				line += "  replay 200"
			}
		}
		fmt.Println(line)
	}
	fmt.Printf("verification failures: %d", verifyFailures)
	if replayURL != "" {
		fmt.Printf("   replay failures: %d", replayFailures)
	}
	fmt.Println()
	if verifyFailures > 0 || replayFailures > 0 {
		return 2
	}
	return 0
}

func post(webhook, signedPayload string) (int, error) {
	body, _ := json.Marshal(map[string]string{"signedPayload": signedPayload})
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Post(webhook, "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}
