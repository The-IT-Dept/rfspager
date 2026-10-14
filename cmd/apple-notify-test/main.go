// apple-notify-test asks Apple to send a TEST App Store Server Notification
// to the URL configured in App Store Connect and reports every delivery
// attempt, so a URL change can be verified without waiting for a renewal.
//
// Run it on apps01 from /opt/rfspager-go so it reads the server's .env:
//
//	./apple-notify-test [-env Production|Sandbox]
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/the-it-dept/rfspager/internal/iap"
)

func main() {
	env := flag.String("env", "Production", "Production or Sandbox: which App Store Connect URL to exercise")
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
	token, err := client.RequestTestNotification(*env)
	if err != nil {
		fmt.Fprintln(os.Stderr, "request failed:", err)
		os.Exit(1)
	}
	fmt.Printf("%s: test notification requested, token %s\n", *env, token)
	for i := 0; i < 18; i++ {
		time.Sleep(5 * time.Second)
		st, err := client.TestNotificationStatus(*env, token)
		if err != nil {
			if strings.Contains(err.Error(), "404") {
				fmt.Println("  not processed yet")
				continue
			}
			fmt.Fprintln(os.Stderr, "status failed:", err)
			os.Exit(1)
		}
		if len(st.SendAttempts) == 0 {
			fmt.Println("  no delivery attempt yet")
			continue
		}
		for _, a := range st.SendAttempts {
			fmt.Printf("  %s  %s\n", time.UnixMilli(a.AttemptDate).UTC().Format(time.RFC3339), a.SendAttemptResult)
		}
		return
	}
	fmt.Println("gave up waiting; query again later with the token above")
	os.Exit(2)
}
