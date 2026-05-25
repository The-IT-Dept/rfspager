// Local development tool: fires synthetic messages at the ingress so you can
// exercise the LISTEN/NOTIFY → fan-out → dispatch → APN/FCM pipeline without
// waiting for a real RFS pager event.
//
//   go run ./cmd/send-test/                 # send to a random subscribed capcode (user 1)
//   go run ./cmd/send-test/ -mode=alertzone # coord inside user 1's first alertzone
//   go run ./cmd/send-test/ -mode=miss      # capcode nobody is subscribed to
//   go run ./cmd/send-test/ -user=42        # target user 42 instead of user 1
//   go run ./cmd/send-test/ -watch          # tail notifications for 30s after sending
//
// Idempotently seeds a `local-test` system + api_key on first run so the
// ingress route accepts the POST.
package main

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	mrand "math/rand"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

const (
	testAPIKey     = "LOCALTEST00000000000000A"
	testSystemName = "local-test"
)

var (
	dsn      = flag.String("dsn", envOr("DATABASE_URL", "postgres://127.0.0.1:5432/rfspager?sslmode=disable"), "Postgres DSN")
	baseURL  = flag.String("url", envOr("SERVER_URL", "http://127.0.0.1:8080"), "Server base URL")
	mode     = flag.String("mode", "capcode", "capcode | alertzone | miss")
	userID   = flag.Int64("user", 1, "User id whose subscriptions / alertzones drive the test")
	text     = flag.String("text", "", "Message body (defaults to a per-mode preset)")
	apiKey   = flag.String("api-key", testAPIKey, "Ingress api key")
	watch    = flag.Bool("watch", false, "Tail the notifications table for 30s after sending")
	insecure = flag.Bool("insecure", false, "Skip the local system seed step (use an existing key)")
)

func main() {
	flag.Parse()

	conn := sqlx.MustConnect("postgres", *dsn)
	defer conn.Close()

	if !*insecure {
		ensureSeedSystem(conn)
	}

	var (
		address  string
		message  string
		hint     string
	)

	switch *mode {
	case "capcode":
		c := pickSubscribedCapcode(conn, *userID)
		address = c.address
		message = nonEmpty(*text, fmt.Sprintf("TEST %s — %s — sender %s", c.alias, time.Now().Format("15:04:05"), shortID()))
		hint = fmt.Sprintf("user %d is subscribed to capcode %d (%s / %s)", *userID, c.id, c.alias, c.agency)

	case "alertzone":
		az := pickAlertzone(conn, *userID)
		lat, lng := randomPointWithin(az.lat, az.lng, az.radius)
		coord := fmt.Sprintf("[%.6f,%.6f]", lng, lat) // ingester expects [lng,lat]
		address = "0000000"                          // any address; the alertzone match is geo, not capcode
		message = nonEmpty(*text, fmt.Sprintf("TEST alertzone %s @ %s ETA 5 mins", az.name, coord))
		if !contains(message, "[") {
			message += " " + coord
		}
		hint = fmt.Sprintf("user %d alertzone %q centre=(%.4f,%.4f) radius=%dkm — synthesised coord (%.4f,%.4f)",
			*userID, az.name, az.lat, az.lng, az.radius, lat, lng)

	case "miss":
		address = "9999999"
		message = nonEmpty(*text, fmt.Sprintf("TEST miss — nobody subscribed @ %s", time.Now().Format("15:04:05")))
		hint = "synthetic capcode nobody is subscribed to; expect 0 notifications"

	default:
		fail("unknown -mode=%s (capcode | alertzone | miss)", *mode)
	}

	fmt.Println(hint)

	body, _ := json.Marshal(map[string]any{
		"address":  address,
		"message":  message,
		"datetime": fmt.Sprintf("%d", time.Now().Unix()),
		"source":   "send-test",
	})
	req, _ := http.NewRequest("POST", *baseURL+"/api/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", *apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fail("POST failed: %v", err)
	}
	out, _ := readJSON(resp)
	resp.Body.Close()
	fmt.Printf("→ POST /api/messages → %d %s  status=%v\n", resp.StatusCode, http.StatusText(resp.StatusCode), out["status"])
	if resp.StatusCode != 200 {
		os.Exit(1)
	}

	msgID, _ := strconv.ParseInt(fmt.Sprint(out["status"]), 10, 64)

	if *watch && msgID > 0 {
		watchNotifications(conn, msgID)
	} else if msgID > 0 {
		// Quick one-shot count after a 1s settle.
		time.Sleep(1 * time.Second)
		summariseNotifications(conn, msgID)
	}
}

// --- seeding -----------------------------------------------------------------

func ensureSeedSystem(conn *sqlx.DB) {
	// Need a user to own the system; pick any user, fall back to creating one.
	var ownerID int64
	if err := conn.Get(&ownerID, `SELECT id FROM users ORDER BY id LIMIT 1`); err != nil {
		fail("no users in local DB: %v (sign up via the app first, or seed manually)", err)
	}

	_, err := conn.Exec(`
		INSERT INTO systems (system_id, user_id, name, sync, frequency, steal_capcodes, api_key, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2, false, 0, '0', $3, NOW(), NOW())
		ON CONFLICT DO NOTHING`,
		ownerID, testSystemName, testAPIKey)
	if err != nil {
		fail("seed system insert failed: %v", err)
	}

	// Make sure the api_key column is set even if a row already existed.
	if _, err := conn.Exec(`UPDATE systems SET api_key = $1 WHERE name = $2 AND (api_key IS NULL OR api_key = '')`, testAPIKey, testSystemName); err != nil {
		fail("seed system update failed: %v", err)
	}
}

// --- target lookup -----------------------------------------------------------

type capcodeRow struct {
	id     int64
	address string
	alias  string
	agency string
}

func pickSubscribedCapcode(conn *sqlx.DB, uid int64) capcodeRow {
	rows, err := conn.Queryx(`
		SELECT c.id, c.address, c.alias, COALESCE(a.agency, '?') AS agency
		  FROM subscriptions s
		  JOIN capcodes c ON c.id = s.capcode_id
		  LEFT JOIN agencies a ON a.id = c.agency_id
		 WHERE s.user_id = $1
		 ORDER BY c.id
	`, uid)
	if err != nil {
		fail("query subscriptions: %v", err)
	}
	defer rows.Close()
	var pool []capcodeRow
	for rows.Next() {
		var c capcodeRow
		if err := rows.Scan(&c.id, &c.address, &c.alias, &c.agency); err == nil {
			pool = append(pool, c)
		}
	}
	if len(pool) == 0 {
		fail("user %d has no subscriptions; subscribe to a capcode in the app first", uid)
	}
	return pool[mrand.Intn(len(pool))]
}

type alertzoneRow struct {
	id     int64
	name   string
	lat    float64
	lng    float64
	radius int
}

func pickAlertzone(conn *sqlx.DB, uid int64) alertzoneRow {
	var az alertzoneRow
	err := conn.QueryRowx(`SELECT id, name, lat, lng, radius FROM alertzones WHERE user_id = $1 ORDER BY id LIMIT 1`, uid).
		Scan(&az.id, &az.name, &az.lat, &az.lng, &az.radius)
	if err == sql.ErrNoRows {
		fail("user %d has no alertzones; create one in the app first", uid)
	}
	if err != nil {
		fail("query alertzones: %v", err)
	}
	return az
}

// randomPointWithin returns a uniformly-distributed lat/lng inside a circle
// of the given radius (km) around (centerLat, centerLng).
func randomPointWithin(centerLat, centerLng float64, radiusKm int) (float64, float64) {
	r := float64(radiusKm) / 111.0 * math.Sqrt(mrand.Float64()) // approx deg
	θ := mrand.Float64() * 2 * math.Pi
	dLat := r * math.Cos(θ)
	dLng := r * math.Sin(θ) / math.Cos(centerLat*math.Pi/180)
	return centerLat + dLat, centerLng + dLng
}

// --- result inspection ------------------------------------------------------

func summariseNotifications(conn *sqlx.DB, msgID int64) {
	var total, pushed, failed, pending int
	conn.Get(&total, `SELECT count(*) FROM notifications WHERE message_id = $1`, msgID)
	conn.Get(&pushed, `SELECT count(*) FROM notifications WHERE message_id = $1 AND pushed_at IS NOT NULL`, msgID)
	conn.Get(&failed, `SELECT count(*) FROM notifications WHERE message_id = $1 AND push_failed_at IS NOT NULL`, msgID)
	conn.Get(&pending, `SELECT count(*) FROM notifications WHERE message_id = $1 AND push_required AND pushed_at IS NULL AND push_failed_at IS NULL`, msgID)
	fmt.Printf("    notifications: total=%d pushed=%d failed=%d pending=%d\n", total, pushed, failed, pending)
}

func watchNotifications(conn *sqlx.DB, msgID int64) {
	fmt.Println("watching notifications for 30s (Ctrl-C to stop)…")
	deadline := time.Now().Add(30 * time.Second)
	var lastTotal, lastPushed, lastFailed int
	for time.Now().Before(deadline) {
		var total, pushed, failed, pending int
		conn.Get(&total, `SELECT count(*) FROM notifications WHERE message_id = $1`, msgID)
		conn.Get(&pushed, `SELECT count(*) FROM notifications WHERE message_id = $1 AND pushed_at IS NOT NULL`, msgID)
		conn.Get(&failed, `SELECT count(*) FROM notifications WHERE message_id = $1 AND push_failed_at IS NOT NULL`, msgID)
		conn.Get(&pending, `SELECT count(*) FROM notifications WHERE message_id = $1 AND push_required AND pushed_at IS NULL AND push_failed_at IS NULL`, msgID)
		if total != lastTotal || pushed != lastPushed || failed != lastFailed {
			fmt.Printf("    %s  total=%d pushed=%d failed=%d pending=%d\n",
				time.Now().Format("15:04:05"), total, pushed, failed, pending)
			lastTotal, lastPushed, lastFailed = total, pushed, failed
		}
		if total > 0 && pending == 0 {
			// Done — print any errors before exiting.
			rows, _ := conn.Queryx(`SELECT notifiable_id, push_attempts, push_last_error FROM notifications WHERE message_id = $1 AND push_last_error IS NOT NULL`, msgID)
			for rows.Next() {
				var uid int64
				var attempts int
				var errMsg sql.NullString
				rows.Scan(&uid, &attempts, &errMsg)
				fmt.Printf("    error: user=%d attempts=%d err=%s\n", uid, attempts, errMsg.String)
			}
			rows.Close()
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// --- utilities --------------------------------------------------------------

func readJSON(resp *http.Response) (map[string]any, error) {
	var out map[string]any
	err := json.NewDecoder(resp.Body).Decode(&out)
	return out, err
}

func nonEmpty(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func shortID() string {
	var b [3]byte
	rand.Read(b[:])
	return fmt.Sprintf("%x", b)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "send-test: "+format+"\n", args...)
	os.Exit(1)
}
