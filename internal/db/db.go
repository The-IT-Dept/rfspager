package db

import (
	"context"
	"crypto/md5"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/the-it-dept/rfspager/internal/models"
)

type DB struct {
	conn *sqlx.DB
}

func New(databaseURL string) (*DB, error) {
	conn, err := sqlx.Connect("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}
	conn.SetMaxOpenConns(25)
	conn.SetMaxIdleConns(5)
	conn.SetConnMaxLifetime(5 * time.Minute)
	// Allow SELECT * to return columns we don't model — the systems table
	// (and others) have grown columns over time that aren't in our structs.
	return &DB{conn: conn.Unsafe()}, nil
}

func (d *DB) Close() error {
	return d.conn.Close()
}

// Conn returns the underlying sqlx.DB for use by other packages.
func (d *DB) Conn() *sqlx.DB {
	return d.conn
}

func (d *DB) Ping(ctx context.Context) error {
	return d.conn.PingContext(ctx)
}

// GetSystemByUUID finds a system by its system_id (UUID), excluding soft-deleted.
func (d *DB) GetSystemByUUID(ctx context.Context, systemID string) (*models.System, error) {
	var s models.System
	err := d.conn.GetContext(ctx, &s,
		`SELECT * FROM systems WHERE system_id = $1 AND deleted_at IS NULL`, systemID)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// GetSystemByID finds a system by its primary-key id.
func (d *DB) GetSystemByID(ctx context.Context, id int64) (*models.System, error) {
	var s models.System
	err := d.conn.GetContext(ctx, &s,
		`SELECT * FROM systems WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// GetSystemByAPIKey finds a system by its api_key column. Used by the
// Pagermon-compatible ingress at POST /api/messages, where the API key is
// supplied in the apikey header instead of the URL.
func (d *DB) GetSystemByAPIKey(ctx context.Context, apiKey string) (*models.System, error) {
	var s models.System
	err := d.conn.GetContext(ctx, &s,
		`SELECT * FROM systems WHERE api_key = $1 AND deleted_at IS NULL AND approved_at IS NOT NULL AND user_id IS NOT NULL`, apiKey)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// GetSystemByTokenID finds a system by its personal_access_tokens ID (for Sanctum auth).
func (d *DB) GetSystemByTokenID(ctx context.Context, tokenID int64) (*models.System, error) {
	var s models.System
	err := d.conn.GetContext(ctx, &s,
		`SELECT s.* FROM systems s
		 JOIN personal_access_tokens pat ON pat.tokenable_id = s.id AND pat.tokenable_type = 'App\\Models\\System'
		 WHERE pat.id = $1 AND s.deleted_at IS NULL`, tokenID)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// FindCapcodeByAddress finds the best matching capcode using SQL LIKE pattern matching.
// Capcodes use _ as single-char wildcards in their address field.
func (d *DB) FindCapcodeByAddress(ctx context.Context, address string) (*models.Capcode, error) {
	var c models.Capcode
	err := d.conn.GetContext(ctx, &c,
		`SELECT * FROM capcodes
		 WHERE $1 LIKE address
		 AND deleted_at IS NULL
		 ORDER BY REPLACE(address, '_', '%') DESC
		 LIMIT 1`, address)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// CreateAgency creates or finds an agency by name.
func (d *DB) CreateAgency(ctx context.Context, name string) (int64, error) {
	var id int64
	err := d.conn.QueryRowContext(ctx,
		`INSERT INTO agencies (agency, created_at, updated_at) VALUES ($1, NOW(), NOW())
		 ON CONFLICT (agency) DO UPDATE SET agency = EXCLUDED.agency
		 RETURNING id`, name).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// CreateCapcode creates a new capcode.
func (d *DB) CreateCapcode(ctx context.Context, c *models.Capcode) error {
	return d.conn.QueryRowContext(ctx,
		`INSERT INTO capcodes (address, alias, agency_id, icon, colour, "ignore", created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW()) RETURNING id`,
		c.Address, c.Alias, c.AgencyID, c.Icon, c.Colour, c.Ignore).Scan(&c.ID)
}

// IsDuplicateMessage checks whether the incoming message has already been
// stored for this capcode recently. Two checks layered:
//
//  1. Exact byte-for-byte match (MD5) within ±5 min. Catches POCSAG's normal
//     re-transmission pattern where dispatch repeats the same page two or
//     three times for redundancy.
//
//  2. Same-capcode prefix match (first 30 chars) within ±30 s. Catches the
//     case where the receiver decoded a re-transmission with a corrupted
//     tail and lost the last few words — the MD5 differs but the message
//     is clearly the same dispatch. Real distinct messages to the same
//     capcode within 30 s would have to share the entire first 30
//     characters, which RFS and FRNSW dispatch formats never do (RFS
//     embeds the timestamp at chars 12–20; FRNSW puts the incident type
//     starting at char 12 — both discriminate inside the prefix).
func (d *DB) IsDuplicateMessage(ctx context.Context, address, message string, timestamp time.Time) (bool, error) {
	hash := fmt.Sprintf("%x", md5.Sum([]byte(message)))
	start := timestamp.Add(-5 * time.Minute)
	end := timestamp.Add(5 * time.Minute)

	var exists bool
	if err := d.conn.GetContext(ctx, &exists,
		`SELECT EXISTS(
			SELECT 1 FROM messages
			WHERE address = $1 AND md5 = $2
			AND created_at BETWEEN $3 AND $4
		)`, address, hash, start, end); err != nil {
		return false, err
	}
	if exists {
		return true, nil
	}

	// Prefix-match second pass. Skip for very short messages (the MD5
	// pass already covers them and short prefixes would over-collide).
	if len(message) < 12 {
		return false, nil
	}
	prefix := message
	if len(prefix) > 30 {
		prefix = prefix[:30]
	}
	prefixStart := timestamp.Add(-30 * time.Second)
	prefixEnd := timestamp.Add(30 * time.Second)
	err := d.conn.GetContext(ctx, &exists,
		`SELECT EXISTS(
			SELECT 1 FROM messages
			WHERE address = $1
			  AND substr(message, 1, 30) = $2
			  AND created_at BETWEEN $3 AND $4
		)`, address, prefix, prefixStart, prefixEnd)
	return exists, err
}

// CreateMessage inserts a new message and returns it with its ID.
func (d *DB) CreateMessage(ctx context.Context, msg *models.Message) error {
	msg.MD5 = fmt.Sprintf("%x", md5.Sum([]byte(msg.Message)))
	msg.UpdatedAt = time.Now()

	return d.conn.QueryRowContext(ctx,
		`INSERT INTO messages (system_id, capcode_id, address, message, md5, lat, lng, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		msg.SystemID, msg.CapcodeID, msg.Address, msg.Message, msg.MD5,
		msg.Lat, msg.Lng, msg.CreatedAt, msg.UpdatedAt).Scan(&msg.ID)
}

// GetSubscribedUsers returns user IDs subscribed to a given capcode, filtered to active subscribers.
func (d *DB) GetSubscribedUsers(ctx context.Context, capcodeID int64) ([]int64, error) {
	var userIDs []int64
	err := d.conn.SelectContext(ctx, &userIDs,
		`SELECT DISTINCT s.user_id FROM subscriptions s
		 JOIN users u ON u.id = s.user_id
		 WHERE s.capcode_id = $1 AND u.subscription_valid = true`, capcodeID)
	return userIDs, err
}

// GetAlertzoneUsers returns user IDs whose alertzones contain the given lat/lng, filtered to active subscribers.
func (d *DB) GetAlertzoneUsers(ctx context.Context, lat, lng float64) ([]int64, error) {
	var userIDs []int64
	err := d.conn.SelectContext(ctx, &userIDs,
		`SELECT DISTINCT a.user_id FROM alertzones a
		 JOIN users u ON u.id = a.user_id
		 WHERE u.subscription_valid = true
		 AND ST_DWithin(
			ST_SetSRID(ST_MakePoint(a.lng, a.lat), 4326)::geography,
			ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,
			a.radius * 1000
		 )`, lng, lat)
	return userIDs, err
}

// GetPushTokensForUsers returns all push notification targets for the given user IDs.
func (d *DB) GetPushTokensForUsers(ctx context.Context, userIDs []int64) ([]models.NotificationTarget, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}

	query, args, err := sqlx.In(
		`SELECT user_id, push_type, push_token FROM installations
		 WHERE user_id IN (?) AND push_token IS NOT NULL AND deleted_at IS NULL`,
		userIDs)
	if err != nil {
		return nil, err
	}

	query = d.conn.Rebind(query)
	var targets []models.NotificationTarget
	err = d.conn.SelectContext(ctx, &targets, query, args...)
	return targets, err
}

// GetUnreadNotificationCount returns the count of unread notifications for a user.
func (d *DB) GetUnreadNotificationCount(ctx context.Context, userID int64) (int, error) {
	var count int
	err := d.conn.GetContext(ctx, &count,
		`SELECT COUNT(*) FROM notifications WHERE notifiable_id = $1 AND read_at IS NULL`, userID)
	return count, err
}

// InsertDatabaseNotification creates a notification record in the notifications table (Laravel format).
func (d *DB) InsertDatabaseNotification(ctx context.Context, id string, userID int64, notifType string, data []byte) error {
	_, err := d.conn.ExecContext(ctx,
		`INSERT INTO notifications (id, type, notifiable_type, notifiable_id, data, created_at, updated_at)
		 VALUES ($1, $2, 'App\Models\User', $3, $4, NOW(), NOW())`,
		id, notifType, userID, data)
	return err
}

// GetCapcodeWithAgency fetches a capcode by its ID, joined with its agency name.
func (d *DB) GetCapcodeWithAgency(ctx context.Context, id int64) (*models.CapcodeWithAgency, error) {
	var c models.CapcodeWithAgency
	err := d.conn.GetContext(ctx, &c,
		`SELECT c.*, a.agency AS agency_name
		 FROM capcodes c
		 LEFT JOIN agencies a ON a.id = c.agency_id
		 WHERE c.id = $1 AND c.deleted_at IS NULL`, id)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// GetSystemBySanctumToken finds a system by validating a Sanctum token.
func (d *DB) GetSystemBySanctumToken(ctx context.Context, tokenID string, hashedToken string) (*models.System, error) {
	var s models.System
	err := d.conn.GetContext(ctx, &s,
		`SELECT s.* FROM systems s
		 JOIN personal_access_tokens pat ON pat.tokenable_id = s.id AND pat.tokenable_type = 'App\Models\System'
		 WHERE pat.id = $1 AND pat.token = $2 AND s.deleted_at IS NULL`, tokenID, hashedToken)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// --- system_connections ---

// SystemConnection is one row in system_connections, plus the joined system
// info for convenient admin display.
type SystemConnection struct {
	SystemID         int64          `db:"system_id" json:"system_id"`
	PodID            string         `db:"pod_id" json:"pod_id"`
	ConnectedAt      time.Time      `db:"connected_at" json:"connected_at"`
	LastPingAt       time.Time      `db:"last_ping_at" json:"last_ping_at"`
	ReceiverVersion  sql.NullString `db:"receiver_version" json:"receiver_version,omitempty"`
	ReceiverOS       sql.NullString `db:"receiver_os" json:"receiver_os,omitempty"`
	ReceiverArch     sql.NullString `db:"receiver_arch" json:"receiver_arch,omitempty"`
	ReceiverHostname sql.NullString `db:"receiver_hostname" json:"receiver_hostname,omitempty"`
	Dongles          []byte         `db:"dongles" json:"dongles,omitempty"` // raw JSONB
}

// UpsertSystemConnection records (or replaces) a receiver's connection. The
// PRIMARY KEY on system_id means a new pod taking over evicts any prior row;
// the previous holding pod's heartbeat will then return 0 rows affected and
// the local socket should be dropped.
func (d *DB) UpsertSystemConnection(ctx context.Context, c *SystemConnection) error {
	_, err := d.conn.ExecContext(ctx,
		`INSERT INTO system_connections
		   (system_id, pod_id, connected_at, last_ping_at,
		    receiver_version, receiver_os, receiver_arch, receiver_hostname, dongles)
		 VALUES ($1, $2, NOW(), NOW(), $3, $4, $5, $6, $7)
		 ON CONFLICT (system_id) DO UPDATE SET
		    pod_id = EXCLUDED.pod_id,
		    connected_at = EXCLUDED.connected_at,
		    last_ping_at = EXCLUDED.last_ping_at,
		    receiver_version = EXCLUDED.receiver_version,
		    receiver_os = EXCLUDED.receiver_os,
		    receiver_arch = EXCLUDED.receiver_arch,
		    receiver_hostname = EXCLUDED.receiver_hostname,
		    dongles = EXCLUDED.dongles`,
		c.SystemID, c.PodID,
		c.ReceiverVersion, c.ReceiverOS, c.ReceiverArch, c.ReceiverHostname, c.Dongles)
	return err
}

// TouchSystemConnection bumps last_ping_at iff the current row still belongs
// to this pod. Returns (true, nil) if our row was bumped; (false, nil) means
// another pod has taken over and the caller should close its local socket.
func (d *DB) TouchSystemConnection(ctx context.Context, systemID int64, podID string) (bool, error) {
	res, err := d.conn.ExecContext(ctx,
		`UPDATE system_connections SET last_ping_at = NOW()
		 WHERE system_id = $1 AND pod_id = $2`, systemID, podID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeleteSystemConnection removes the row iff it still belongs to this pod.
// A no-op if another pod has already taken over.
func (d *DB) DeleteSystemConnection(ctx context.Context, systemID int64, podID string) error {
	_, err := d.conn.ExecContext(ctx,
		`DELETE FROM system_connections WHERE system_id = $1 AND pod_id = $2`,
		systemID, podID)
	return err
}

// PruneStaleSystemConnections deletes rows whose heartbeat is older than the
// given cutoff. Safe to call from any pod — the row owner has already given
// up, otherwise its heartbeat would have refreshed it.
func (d *DB) PruneStaleSystemConnections(ctx context.Context, olderThan time.Duration) (int64, error) {
	res, err := d.conn.ExecContext(ctx,
		`DELETE FROM system_connections WHERE last_ping_at < NOW() - $1::interval`,
		fmt.Sprintf("%d seconds", int(olderThan.Seconds())))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// GetSystemConnection returns the live connection for a system, or nil if
// the receiver is offline.
func (d *DB) GetSystemConnection(ctx context.Context, systemID int64) (*SystemConnection, error) {
	var c SystemConnection
	err := d.conn.GetContext(ctx, &c,
		`SELECT * FROM system_connections WHERE system_id = $1`, systemID)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ListSystemConnections returns all live receiver connections.
func (d *DB) ListSystemConnections(ctx context.Context) ([]SystemConnection, error) {
	var out []SystemConnection
	err := d.conn.SelectContext(ctx, &out,
		`SELECT * FROM system_connections ORDER BY last_ping_at DESC`)
	return out, err
}

// CreateSystemCheckin records a system checkin.
func (d *DB) CreateSystemCheckin(ctx context.Context, systemID int64, ipAddress string, data map[string]any) error {
	_, err := d.conn.ExecContext(ctx,
		`INSERT INTO system_checkins (system_id, ip_address, created_at, updated_at)
		 VALUES ($1, $2, NOW(), NOW())`, systemID, ipAddress)
	if err != nil {
		slog.Error("failed to create system checkin", "error", err)
	}
	return err
}
