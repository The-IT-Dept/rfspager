// google-sub-check compares every Google Play subscription prod believes is
// live (or recently lapsed) with what the Play Developer API says now. It is
// read-only: it prints mismatches and never writes. Run it on apps01 from
// /opt/rfspager-go so it reads the server's .env (DATABASE_URL,
// GOOGLE_PLAY_CREDENTIALS, GOOGLE_PLAY_PACKAGE).
//
//	./google-sub-check              every valid or recently lapsed Google subscriber
//	./google-sub-check -user 123    one user
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/the-it-dept/rfspager/internal/iap"
)

type row struct {
	ID        int64      `db:"id"`
	Token     string     `db:"google_purchase_token"`
	ProductID *string    `db:"subscription_product_id"`
	Valid     bool       `db:"subscription_valid"`
	Expiry    *time.Time `db:"subscription_expiry"`
	WillRenew *bool      `db:"will_renew"`
}

func main() {
	user := flag.Int64("user", 0, "check this user id only")
	flag.Parse()
	_ = godotenv.Load()
	pkg := os.Getenv("GOOGLE_PLAY_PACKAGE")
	if pkg == "" {
		pkg = "io.devlan.rfspager"
	}
	ctx := context.Background()
	client, err := iap.NewGoogleClient(ctx, os.Getenv("GOOGLE_PLAY_CREDENTIALS"), pkg)
	if err != nil || client == nil {
		fmt.Fprintln(os.Stderr, "Google Play client:", err)
		os.Exit(1)
	}
	db, err := sqlx.Connect("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "database:", err)
		os.Exit(1)
	}
	q := `SELECT id, google_purchase_token, subscription_product_id, subscription_valid, subscription_expiry, will_renew
	        FROM users WHERE google_purchase_token IS NOT NULL AND subscription_platform = 'google'
	         AND (subscription_valid OR subscription_expiry > NOW() - INTERVAL '60 days')`
	args := []any{}
	if *user != 0 {
		q = `SELECT id, google_purchase_token, subscription_product_id, subscription_valid, subscription_expiry, will_renew
		       FROM users WHERE id = $1 AND google_purchase_token IS NOT NULL`
		args = append(args, *user)
	}
	var rows []row
	if err := db.SelectContext(ctx, &rows, q+" ORDER BY id", args...); err != nil {
		fmt.Fprintln(os.Stderr, "query:", err)
		os.Exit(1)
	}
	fmt.Printf("checking %d Google subscription(s) against the Play API\n", len(rows))
	var same, mismatch, apiErr int
	for _, r := range rows {
		product := ""
		if r.ProductID != nil {
			product = *r.ProductID
		}
		res, err := client.ValidatePurchase(ctx, r.Token, product)
		if err != nil {
			apiErr++
			fmt.Printf("  user %-6d prod valid=%-5v expiry=%s   PLAY API ERROR: %v\n", r.ID, r.Valid, fmtTime(r.Expiry), err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		expiryDiffers := (r.Expiry == nil) != (res.ExpiresAt == nil) ||
			(r.Expiry != nil && res.ExpiresAt != nil && r.Expiry.Sub(*res.ExpiresAt).Abs() > time.Minute)
		if r.Valid != res.Valid || expiryDiffers {
			mismatch++
			fmt.Printf("  user %-6d prod valid=%-5v expiry=%s   google valid=%-5v expiry=%s renews=%v product=%s\n",
				r.ID, r.Valid, fmtTime(r.Expiry), res.Valid, fmtTime(res.ExpiresAt), res.WillRenew, res.ProductID)
		} else {
			same++
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Printf("in sync: %d   mismatched: %d   api errors: %d\n", same, mismatch, apiErr)
	if mismatch > 0 || apiErr > 0 {
		os.Exit(2)
	}
}

func fmtTime(t *time.Time) string {
	if t == nil {
		return "none"
	}
	return t.UTC().Format("2006-01-02T15:04")
}
