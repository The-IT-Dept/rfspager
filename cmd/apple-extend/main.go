// apple-extend gives an Apple subscriber free time by pushing their next
// renewal date back through the App Store Server API, so Apple doesn't charge
// them until the free time runs out. Changing subscription_expiry in the
// database instead would do nothing for an auto-renewing subscriber: Apple
// still charges on the original date.
//
// Run it on apps01 from /opt/rfspager-go so it reads the server's .env:
//
//	./apple-extend -otx 500001320546624 -days 90
//	./apple-extend -otx 500001320546624 -days 90 -reason 1
//
// -days is at most 90 per call and Apple allows two extensions per
// subscription in any 365 days, so two calls give about six months. Each call
// uses a fresh request id; Apple then sends RENEWAL_EXTENDED and the webhook
// moves subscription_expiry. -reason is Apple's code: 0 undeclared,
// 1 customer satisfaction, 2 other, 3 service issue or outage (default).
package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/the-it-dept/rfspager/internal/iap"
)

func main() {
	env := flag.String("env", "Production", "Production or Sandbox")
	otx := flag.String("otx", "", "original transaction id (users.apple_original_transaction_id)")
	days := flag.Int("days", 0, "days to extend by, 1-90")
	reason := flag.Int("reason", iap.ExtendReasonServiceIssue, "Apple extend reason code")
	flag.Parse()
	if *otx == "" || *days < 1 || *days > 90 {
		fmt.Fprintln(os.Stderr, "usage: apple-extend -otx ORIGINAL_TRANSACTION_ID -days 1-90 [-reason 0-3] [-env Sandbox]")
		os.Exit(2)
	}
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
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	requestID := hex.EncodeToString(id)
	res, err := client.ExtendRenewalDate(*env, *otx, *days, *reason, requestID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "extend failed (request %s): %v\n", requestID, err)
		os.Exit(1)
	}
	fmt.Printf("%s: %s extended by %d days, renews %s (request %s, web order line item %s)\n",
		*env, res.OriginalTransactionID, *days, time.UnixMilli(res.EffectiveDate).UTC().Format(time.RFC3339), requestID, res.WebOrderLineItemID)
}
