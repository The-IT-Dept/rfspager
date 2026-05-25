package db

import (
	"context"
	"crypto/md5"
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
	return &DB{conn: conn}, nil
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

// GetSystemByAPIKey finds a system by its api_key column. Used by the
// Pagermon-compatible ingress at POST /api/messages, where the API key is
// supplied in the apikey header instead of the URL.
func (d *DB) GetSystemByAPIKey(ctx context.Context, apiKey string) (*models.System, error) {
	var s models.System
	err := d.conn.GetContext(ctx, &s,
		`SELECT * FROM systems WHERE api_key = $1 AND deleted_at IS NULL`, apiKey)
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

// IsDuplicateMessage checks if a message with the same address and content
// was received within a 10-minute window.
func (d *DB) IsDuplicateMessage(ctx context.Context, address, message string, timestamp time.Time) (bool, error) {
	hash := fmt.Sprintf("%x", md5.Sum([]byte(message)))
	start := timestamp.Add(-5 * time.Minute)
	end := timestamp.Add(5 * time.Minute)

	var exists bool
	err := d.conn.GetContext(ctx, &exists,
		`SELECT EXISTS(
			SELECT 1 FROM messages
			WHERE address = $1 AND md5 = $2
			AND created_at BETWEEN $3 AND $4
		)`, address, hash, start, end)
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
