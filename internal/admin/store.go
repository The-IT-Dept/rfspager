package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/the-it-dept/rfspager/internal/hostcomp"
	"golang.org/x/crypto/bcrypt"
)

type Store struct {
	db *sqlx.DB

	statsCacheMu sync.RWMutex
	statsCache   map[string]statsCacheEntry
}

type statsCacheEntry struct {
	value     any
	expiresAt time.Time
}

func NewStore(db *sqlx.DB) *Store {
	return &Store{db: db, statsCache: map[string]statsCacheEntry{}}
}

// cachedStats returns a cached value if present and unexpired, otherwise calls
// fetch, stores the result, and returns it. The cache is per-process and best
// suited for headline numbers where a few seconds of staleness is acceptable.
func (s *Store) cachedStats(key string, ttl time.Duration, fetch func() (any, error)) (any, error) {
	s.statsCacheMu.RLock()
	entry, ok := s.statsCache[key]
	s.statsCacheMu.RUnlock()
	if ok && time.Now().Before(entry.expiresAt) {
		return entry.value, nil
	}

	val, err := fetch()
	if err != nil {
		return nil, err
	}

	s.statsCacheMu.Lock()
	s.statsCache[key] = statsCacheEntry{value: val, expiresAt: time.Now().Add(ttl)}
	s.statsCacheMu.Unlock()
	return val, nil
}

// RunMigration executes the admin_users migration.
func (s *Store) RunMigration(ctx context.Context, sql string) error {
	_, err := s.db.ExecContext(ctx, sql)
	return err
}

// SeedAdmin ensures a default admin user exists. Call on boot.
func (s *Store) SeedAdmin(ctx context.Context, email, password string) error {
	var exists bool
	if err := s.db.GetContext(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM admin_users WHERE email = $1)`, email); err != nil {
		return err
	}
	if exists {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO admin_users (email, password_hash, name, role, active, created_at, updated_at)
		 VALUES ($1, $2, 'Admin', 'superadmin', true, NOW(), NOW())`, email, string(hash))
	return err
}

// --- Admin Users ---

func (s *Store) GetAdminByEmail(ctx context.Context, email string) (*AdminUser, error) {
	var u AdminUser
	err := s.db.GetContext(ctx, &u, `SELECT id, email, password_hash, name, role, active, last_login_at, created_at, updated_at, token_version FROM admin_users WHERE email = $1`, email)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) GetAdminByID(ctx context.Context, id int64) (*AdminUser, error) {
	var u AdminUser
	err := s.db.GetContext(ctx, &u, `SELECT id, email, password_hash, name, role, active, last_login_at, created_at, updated_at, token_version FROM admin_users WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) UpdateAdminLastLogin(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE admin_users SET last_login_at = NOW(), updated_at = NOW() WHERE id = $1`, id)
	return err
}

func (s *Store) RevokeAdminSessions(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE admin_users SET token_version = token_version + 1, updated_at = NOW() WHERE id = $1`, id)
	return err
}

func (s *Store) ListAdminUsers(ctx context.Context, p ListParams) (*PaginatedResult, error) {
	return s.paginatedQuery(ctx, "admin_users", "*", "1=1", nil, p, []string{"email", "name"})
}

// GetAdminUser loads one administrator by id. The console's edit form reads
// this; without it the detail route 405s and the page never renders.
func (s *Store) GetAdminUser(ctx context.Context, id int64) (map[string]any, error) {
	row := s.db.QueryRowxContext(ctx, `SELECT * FROM admin_users WHERE id = $1`, id)
	result := make(map[string]any)
	if err := row.MapScan(result); err != nil {
		return nil, err
	}
	cleanMapScan(result)
	return result, nil
}

func (s *Store) CreateAdminUser(ctx context.Context, data map[string]any) (int64, error) {
	if err := validateFields(data, adminUserInputFields); err != nil {
		return 0, err
	}
	if role, ok := data["role"].(string); ok {
		role = strings.ToLower(strings.TrimSpace(role))
		if role != "admin" && role != "superadmin" {
			return 0, fmt.Errorf("role %q must be admin or superadmin", role)
		}
		data["role"] = role
	}
	if pw, ok := data["password"].(string); ok {
		hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
		if err != nil {
			return 0, err
		}
		data["password_hash"] = string(hash)
		delete(data, "password")
	}
	return s.insertRow(ctx, "admin_users", data)
}

func (s *Store) UpdateAdminUser(ctx context.Context, id int64, data map[string]any) error {
	if err := validateFields(data, adminUserInputFields); err != nil {
		return err
	}
	if role, ok := data["role"].(string); ok {
		role = strings.ToLower(strings.TrimSpace(role))
		if role != "admin" && role != "superadmin" {
			return fmt.Errorf("role %q must be admin or superadmin", role)
		}
		data["role"] = role
	}
	passwordChanged := false
	if pw, ok := data["password"].(string); ok && pw != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		data["password_hash"] = string(hash)
		delete(data, "password")
		passwordChanged = true
	} else {
		delete(data, "password")
	}
	if !passwordChanged {
		return s.updateRow(ctx, "admin_users", id, data)
	}
	if err := validateWritableFields("admin_users", data); err != nil {
		return err
	}
	// A password change and the associated session revocation must commit as a
	// single database operation; otherwise a write failure could leave the new
	// password active while old sessions remain usable.
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	data["updated_at"] = time.Now()
	sets := make([]string, 0, len(data)+1)
	values := make([]any, 0, len(data)+1)
	index := 1
	for key, value := range data {
		sets = append(sets, fmt.Sprintf(`"%s" = $%d`, key, index))
		values = append(values, value)
		index++
	}
	sets = append(sets, "token_version = token_version + 1")
	values = append(values, id)
	query := fmt.Sprintf("UPDATE admin_users SET %s WHERE id = $%d", strings.Join(sets, ", "), index)
	if _, err := tx.ExecContext(ctx, query, values...); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteAdminUser(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_users WHERE id = $1`, id)
	return err
}

// --- Users ---

func (s *Store) ListUsers(ctx context.Context, p ListParams) (*PaginatedResult, error) {
	return s.paginatedQuery(ctx, "users u", `u.*,
		`+userProAccessColumn("u")+`,
		(SELECT COUNT(*) FROM installations i WHERE i.user_id = u.id AND i.deleted_at IS NULL) as installations_count,
		(SELECT COUNT(*) FROM subscriptions s WHERE s.user_id = u.id) as subscriptions_count,
		(SELECT COUNT(*) FROM alertzones a WHERE a.user_id = u.id) as alertzones_count`,
		"1=1", nil, p, []string{"u.name", "u.email"})
}

func (s *Store) GetUser(ctx context.Context, id int64) (map[string]any, error) {
	return s.getRow(ctx, "users", id, "*, "+userProAccessColumn("")+", "+userFreeTimeColumns)
}

// userFreeTimeColumns sums the free time given with "Give free time"
// (applied and partial grants) and when it was last given; NULL when none.
const userFreeTimeColumns = `
	(SELECT SUM(e.days_applied) FROM account_extensions e WHERE e.user_id = users.id AND e.status <> 'failed') AS free_time_days,
	(SELECT MAX(e.created_at) FROM account_extensions e WHERE e.user_id = users.id AND e.status <> 'failed') AS free_time_last_at`

// userProAccessColumn adds pro_access, the user's effective access (a valid
// store subscription OR a running host comp), next to the raw columns: the
// store slot (subscription_*) and the comp (host_comp_until, host_comp_since)
// are both shown as stored.
func userProAccessColumn(alias string) string {
	return hostcomp.Access(alias) + " AS pro_access"
}

func (s *Store) CreateUser(ctx context.Context, data map[string]any) (int64, error) {
	if pw, ok := data["password"].(string); ok {
		hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
		if err != nil {
			return 0, err
		}
		data["password"] = string(hash)
	}
	return s.insertRow(ctx, "users", data)
}

func (s *Store) UpdateUser(ctx context.Context, id int64, data map[string]any) error {
	passwordChanged := false
	if pw, ok := data["password"].(string); ok && pw != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		data["password"] = string(hash)
		passwordChanged = true
	} else {
		delete(data, "password")
	}
	if !passwordChanged {
		return s.updateRow(ctx, "users", id, data)
	}
	if err := validateWritableFields("users", data); err != nil {
		return err
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	data["updated_at"] = time.Now()
	sets := make([]string, 0, len(data)+1)
	values := make([]any, 0, len(data)+1)
	index := 1
	for key, value := range data {
		sets = append(sets, fmt.Sprintf(`"%s" = $%d`, key, index))
		values = append(values, value)
		index++
	}
	sets = append(sets, "token_version = token_version + 1")
	values = append(values, id)
	query := fmt.Sprintf("UPDATE users SET %s WHERE id = $%d", strings.Join(sets, ", "), index)
	if _, err := tx.ExecContext(ctx, query, values...); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE password_reset_tokens SET used_at = NOW() WHERE user_id = $1 AND used_at IS NULL`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	// Dependent rows reference users(id) with the default ON DELETE NO ACTION,
	// so a bare DELETE fails with a foreign-key violation once the user owns
	// anything. Remove the user-owned rows and disown systems (shared history),
	// mirroring the mobile DeleteAccount cleanup.
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	cleanups := []string{
		`DELETE FROM responding         WHERE user_id = $1`,
		`DELETE FROM subscriptions      WHERE user_id = $1`,
		`DELETE FROM filters            WHERE user_id = $1`,
		`DELETE FROM alertzones         WHERE user_id = $1`,
		`DELETE FROM installations      WHERE user_id = $1`,
		`DELETE FROM iap_transactions   WHERE user_id = $1`,
		`DELETE FROM iap_subscription_ownership WHERE user_id = $1`,
		`DELETE FROM photos             WHERE user_id = $1`,
		`DELETE FROM sessions           WHERE user_id = $1`,
		`DELETE FROM social_providers   WHERE user_id = $1`,
		`DELETE FROM password_reset_tokens WHERE user_id = $1`,
		`DELETE FROM personal_access_tokens WHERE tokenable_type = 'App\Models\User' AND tokenable_id = $1`,
		`DELETE FROM notifications      WHERE notifiable_id = $1 AND notifiable_type = 'App\Models\User'`,
		// Don't delete systems the user owns — shared history others rely on.
		`UPDATE systems SET user_id = NULL, api_key = NULL, approved_at = NULL, updated_at = NOW() WHERE user_id = $1`,
	}
	for _, q := range cleanups {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// --- Capcodes ---

func (s *Store) ListCapcodes(ctx context.Context, p ListParams) (*PaginatedResult, error) {
	return s.paginatedQuery(ctx, "capcodes c LEFT JOIN agencies a ON a.id = c.agency_id",
		`c.*, a.agency as agency_name,
		(SELECT COUNT(*) FROM messages m WHERE m.capcode_id = c.id AND m.deleted_at IS NULL) as messages_count,
		(SELECT COUNT(*) FROM subscriptions s WHERE s.capcode_id = c.id) as subscriptions_count`,
		"c.deleted_at IS NULL", nil, p, []string{"c.address", "c.alias", "a.agency"},
		"capcodes c")
}

func (s *Store) GetCapcode(ctx context.Context, id int64) (map[string]any, error) {
	return s.getRow(ctx, "capcodes c LEFT JOIN agencies a ON a.id = c.agency_id",
		id, "c.*, a.agency as agency_name")
}

func (s *Store) CreateCapcode(ctx context.Context, data map[string]any) (int64, error) {
	return s.insertRow(ctx, "capcodes", data)
}

func (s *Store) UpdateCapcode(ctx context.Context, id int64, data map[string]any) error {
	return s.updateRow(ctx, "capcodes", id, data)
}

func (s *Store) DeleteCapcode(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE capcodes SET deleted_at = NOW() WHERE id = $1`, id)
	return err
}

// --- Agencies ---

func (s *Store) ListAgencies(ctx context.Context, p ListParams) (*PaginatedResult, error) {
	return s.paginatedQuery(ctx, "agencies a",
		`a.*, (SELECT COUNT(*) FROM capcodes c WHERE c.agency_id = a.id AND c.deleted_at IS NULL) as capcodes_count`,
		"1=1", nil, p, []string{"a.agency"})
}

func (s *Store) GetAgency(ctx context.Context, id int64) (map[string]any, error) {
	return s.getRow(ctx, "agencies", id, "")
}

func (s *Store) CreateAgency(ctx context.Context, data map[string]any) (int64, error) {
	return s.insertRow(ctx, "agencies", data)
}

func (s *Store) UpdateAgency(ctx context.Context, id int64, data map[string]any) error {
	return s.updateRow(ctx, "agencies", id, data)
}

func (s *Store) DeleteAgency(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM agencies WHERE id = $1`, id)
	return err
}

// --- Systems ---

func (s *Store) ListSystems(ctx context.Context, p ListParams) (*PaginatedResult, error) {
	return s.ListSystemsFiltered(ctx, p, false)
}

// ListSystemsFiltered is the same as ListSystems with an extra `pendingOnly`
// switch used by the admin queue view.
func (s *Store) ListSystemsFiltered(ctx context.Context, p ListParams, pendingOnly bool) (*PaginatedResult, error) {
	where := "s.deleted_at IS NULL"
	if p.IncludeDeleted {
		where = "1=1"
	}
	if pendingOnly {
		where += " AND s.approved_at IS NULL"
	}
	return s.paginatedQuery(ctx,
		"systems s LEFT JOIN users u ON u.id = s.user_id LEFT JOIN system_connections sc ON sc.system_id = s.id",
		systemSelectColumns,
		where, nil, p, []string{"s.name", "s.system_id", "u.name"})
}

func (s *Store) GetSystem(ctx context.Context, id int64) (map[string]any, error) {
	return s.getRow(ctx,
		"systems s LEFT JOIN users u ON u.id = s.user_id LEFT JOIN system_connections sc ON sc.system_id = s.id",
		id, systemSelectColumns)
}

// systemSelectColumns is the column list joined onto system rows for admin
// views. Bundles receiver-agent connection info (online/version/host/dongles)
// alongside the system fields the UI already shows.
var systemSelectColumns = `s.*, u.name as user_name,
	u.host_comp_until as owner_host_comp_until,
	(SELECT MAX(m.created_at) FROM messages m WHERE m.system_id = s.id) as last_message_at,
	sc.pod_id            as receiver_pod_id,
	sc.connected_at      as receiver_connected_at,
	sc.last_ping_at      as receiver_last_ping_at,
	sc.receiver_version  as receiver_version,
	sc.receiver_os       as receiver_os,
	sc.receiver_arch     as receiver_arch,
	sc.receiver_hostname as receiver_hostname,
	sc.dongles::text     as receiver_dongles,
	sc.proto             as receiver_proto,
	sc.config_version    as receiver_config_version,
	sc.config_status     as receiver_config_status,
	sc.host::text        as receiver_host,
	sc.last_stats::text  as receiver_last_stats,
	sc.last_stats_at     as receiver_last_stats_at,
	(sc.system_id IS NOT NULL AND sc.last_ping_at > NOW() - INTERVAL '90 seconds') as receiver_online`

func (s *Store) CreateSystem(ctx context.Context, data map[string]any) (int64, error) {
	blankToNull(data, "lat", "lng", "located_since", "hardware")
	return s.insertRow(ctx, "systems", data)
}

func (s *Store) UpdateSystem(ctx context.Context, id int64, data map[string]any) error {
	blankToNull(data, "lat", "lng", "located_since", "hardware")
	return s.updateRow(ctx, "systems", id, data)
}

// blankToNull turns "" into NULL for typed columns a form may submit empty
// (a cleared location, located_since or hardware), which Postgres would
// otherwise reject as an invalid number or timestamp, or by a CHECK.
func blankToNull(data map[string]any, fields ...string) {
	for _, f := range fields {
		if v, ok := data[f].(string); ok && strings.TrimSpace(v) == "" {
			data[f] = nil
		}
	}
}

func (s *Store) DeleteSystem(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE systems SET deleted_at = NOW() WHERE id = $1`, id)
	return err
}

// --- Messages ---

func (s *Store) ListMessages(ctx context.Context, p ListParams) (*PaginatedResult, error) {
	// Exact COUNT(*) on this 2M+ row table costs ~300ms; use reltuples estimate when unfiltered.
	return s.paginatedQuery(ctx,
		"messages m LEFT JOIN systems sy ON sy.id = m.system_id LEFT JOIN capcodes c ON c.id = m.capcode_id LEFT JOIN agencies a ON a.id = c.agency_id",
		`m.*, sy.name as system_name, c.address as capcode_address, c.alias as capcode_alias, a.agency as agency_name,
		 (SELECT COUNT(*) FROM notifications n WHERE n.message_id = m.id) as notifications_count,
		 (SELECT COUNT(*) FROM notifications n WHERE n.message_id = m.id AND n.pushed_at IS NOT NULL) as notifications_pushed_count`,
		"m.deleted_at IS NULL", nil, p, []string{"m.address", "m.message"},
		approxCountReltuples("messages"))
}

func (s *Store) GetMessage(ctx context.Context, id int64) (map[string]any, error) {
	return s.getRow(ctx,
		"messages m LEFT JOIN systems sy ON sy.id = m.system_id LEFT JOIN capcodes c ON c.id = m.capcode_id LEFT JOIN agencies a ON a.id = c.agency_id",
		id, "m.*, sy.name as system_name, c.address as capcode_address, c.alias as capcode_alias, a.agency as agency_name")
}

func (s *Store) CreateMessage(ctx context.Context, data map[string]any) (int64, error) {
	return s.insertRow(ctx, "messages", data)
}

func (s *Store) UpdateMessage(ctx context.Context, id int64, data map[string]any) error {
	return s.updateRow(ctx, "messages", id, data)
}

func (s *Store) DeleteMessage(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE messages SET deleted_at = NOW() WHERE id = $1`, id)
	return err
}

// MessageNotifications is who was notified for one message: the rows plus
// the totals the rows were cut from.
type MessageNotifications struct {
	Data   []map[string]any `json:"data"`
	Total  int              `json:"total"`
	Pushed int              `json:"pushed"`
	Failed int              `json:"failed"`
}

// ListMessageNotifications lists the users notified for a message, newest
// first, with the text each push carried so the admin page can show when a
// later, cleaner copy of the page replaced what was already sent.
func (s *Store) ListMessageNotifications(ctx context.Context, messageID int64, limit int) (*MessageNotifications, error) {
	out := &MessageNotifications{Data: []map[string]any{}}
	if err := s.db.QueryRowxContext(ctx,
		`SELECT COUNT(*), COUNT(pushed_at), COUNT(push_failed_at)
		   FROM notifications WHERE message_id = $1`, messageID).
		Scan(&out.Total, &out.Pushed, &out.Failed); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryxContext(ctx,
		`SELECT n.id, n.notifiable_id AS user_id, u.email AS user_email, u.name AS user_name,
		        n.created_at, n.pushed_at, n.push_failed_at, n.push_attempts, n.push_required,
		        n.push_last_error, n.read_at,
		        CASE WHEN n.data ~ '^\s*\{' THEN n.data::jsonb->>'message' END AS pushed_message
		   FROM notifications n
		   LEFT JOIN users u ON u.id = n.notifiable_id AND n.notifiable_type = 'App\Models\User'
		  WHERE n.message_id = $1
		  ORDER BY n.created_at DESC, n.id
		  LIMIT $2`, messageID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		m := make(map[string]any)
		if err := rows.MapScan(m); err != nil {
			return nil, err
		}
		cleanMapScan(m)
		out.Data = append(out.Data, m)
	}
	return out, rows.Err()
}

// ListMessageCopies returns every receiver's decode of the page a message
// was stored from: page log lines to the same address heard within five
// minutes before and ten after the message, oldest first, with the copy
// whose text was kept marked.
func (s *Store) ListMessageCopies(ctx context.Context, messageID int64) ([]map[string]any, error) {
	rows, err := s.db.QueryxContext(ctx,
		`SELECT l.id, l.system_id, sy.name AS system_name, l.ts, l.received_at,
		        l.raw->>'channel' AS channel, l.raw->>'msg' AS message,
		        NULLIF(l.raw->>'corrected', '')::int AS corrected,
		        NULLIF(l.raw->>'uncorrectable', '')::int AS uncorrectable,
		        COALESCE((l.raw->>'truncated')::bool, false) AS truncated,
		        btrim(l.raw->>'msg') = btrim(m.message) AS kept
		   FROM messages m
		   JOIN system_logs l
		     ON l.message = 'page'
		    AND l.raw->>'address' = m.address
		    AND l.received_at BETWEEN m.created_at - INTERVAL '5 minutes' AND m.created_at + INTERVAL '10 minutes'
		   LEFT JOIN systems sy ON sy.id = l.system_id
		  WHERE m.id = $1
		  ORDER BY l.ts, l.id`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		m := make(map[string]any)
		if err := rows.MapScan(m); err != nil {
			return nil, err
		}
		cleanMapScan(m)
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListIncidentMessages returns the other messages that share a message's
// incident key within the push-suppression window either side of it, with
// their notification counts, oldest first. Empty when the message has no
// incident key.
func (s *Store) ListIncidentMessages(ctx context.Context, messageID int64) ([]map[string]any, error) {
	rows, err := s.db.QueryxContext(ctx,
		`SELECT m2.id, m2.address, c.alias AS capcode_alias, sy.name AS system_name,
		        m2.created_at, m2.replaced_count, m2.quality, m2.message,
		        (SELECT COUNT(*) FROM notifications n WHERE n.message_id = m2.id) AS notifications_count,
		        (SELECT COUNT(*) FROM notifications n WHERE n.message_id = m2.id AND n.pushed_at IS NOT NULL) AS notifications_pushed_count
		   FROM messages m
		   JOIN messages m2
		     ON m2.incident_key = m.incident_key
		    AND m2.id <> m.id
		    AND m2.deleted_at IS NULL
		    AND m2.created_at BETWEEN m.created_at - INTERVAL '15 minutes' AND m.created_at + INTERVAL '15 minutes'
		   LEFT JOIN capcodes c ON c.id = m2.capcode_id
		   LEFT JOIN systems sy ON sy.id = m2.system_id
		  WHERE m.id = $1 AND m.incident_key IS NOT NULL
		  ORDER BY m2.created_at, m2.id`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		m := make(map[string]any)
		if err := rows.MapScan(m); err != nil {
			return nil, err
		}
		cleanMapScan(m)
		out = append(out, m)
	}
	return out, rows.Err()
}

// --- Installations ---

func (s *Store) ListInstallations(ctx context.Context, p ListParams) (*PaginatedResult, error) {
	return s.paginatedQuery(ctx, "installations i LEFT JOIN users u ON u.id = i.user_id",
		`i.*, u.name as user_name`,
		"i.deleted_at IS NULL", nil, p, []string{"i.id", "i.vendor", "i.model", "u.name"})
}

func (s *Store) GetInstallation(ctx context.Context, id string) (map[string]any, error) {
	row := s.db.QueryRowxContext(ctx,
		`SELECT i.*, u.name as user_name FROM installations i LEFT JOIN users u ON u.id = i.user_id WHERE i.id = $1`, id)
	result := make(map[string]any)
	err := row.MapScan(result)
	if err != nil {
		return nil, err
	}
	cleanMapScan(result)
	return result, nil
}

func (s *Store) DeleteInstallation(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE installations SET deleted_at = NOW() WHERE id = $1`, id)
	return err
}

// --- Subscriptions ---

func (s *Store) ListSubscriptions(ctx context.Context, p ListParams) (*PaginatedResult, error) {
	return s.paginatedQuery(ctx,
		"subscriptions s LEFT JOIN users u ON u.id = s.user_id LEFT JOIN capcodes c ON c.id = s.capcode_id LEFT JOIN agencies a ON a.id = c.agency_id",
		`s.*, u.name as user_name, c.address as capcode_address, c.alias as capcode_alias, a.agency as agency_name`,
		"1=1", nil, p, []string{"u.name", "c.address", "c.alias"})
}

func (s *Store) CreateSubscription(ctx context.Context, data map[string]any) (int64, error) {
	return s.insertRow(ctx, "subscriptions", data)
}

func (s *Store) DeleteSubscription(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM subscriptions WHERE id = $1`, id)
	return err
}

// --- Alertzones ---

func (s *Store) ListAlertzones(ctx context.Context, p ListParams) (*PaginatedResult, error) {
	return s.paginatedQuery(ctx, "alertzones a LEFT JOIN users u ON u.id = a.user_id",
		`a.*, u.name as user_name`,
		"1=1", nil, p, []string{"a.name", "u.name"})
}

func (s *Store) GetAlertzone(ctx context.Context, id int64) (map[string]any, error) {
	return s.getRow(ctx, "alertzones a LEFT JOIN users u ON u.id = a.user_id",
		id, "a.*, u.name as user_name")
}

func (s *Store) CreateAlertzone(ctx context.Context, data map[string]any) (int64, error) {
	return s.insertRow(ctx, "alertzones", data)
}

func (s *Store) UpdateAlertzone(ctx context.Context, id int64, data map[string]any) error {
	return s.updateRow(ctx, "alertzones", id, data)
}

func (s *Store) DeleteAlertzone(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM alertzones WHERE id = $1`, id)
	return err
}

// --- Notifications ---

func (s *Store) ListNotifications(ctx context.Context, p ListParams) (*PaginatedResult, error) {
	return s.paginatedQuery(ctx, "notifications n",
		`n.*`,
		"1=1", nil, p, []string{"n.type", "n.notifiable_id::text"})
}

func (s *Store) MarkNotificationRead(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET read_at = NOW() WHERE id = $1`, id)
	return err
}

func (s *Store) DeleteNotification(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM notifications WHERE id = $1`, id)
	return err
}

// --- Responders ---

func (s *Store) ListResponders(ctx context.Context, p ListParams) (*PaginatedResult, error) {
	return s.paginatedQuery(ctx,
		"responding r LEFT JOIN users u ON u.id = r.user_id LEFT JOIN messages m ON m.id = r.message_id",
		`r.*, u.name as user_name, SUBSTRING(m.message, 1, 80) as message_preview`,
		"1=1", nil, p, []string{"u.name"})
}

func (s *Store) DeleteResponder(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM responding WHERE id = $1`, id)
	return err
}

// --- Capcode Suggestions ---

// ListCapcodeSuggestions includes the target capcode's current alias and
// agency so the reviewer can compare an amendment with what it replaces.
func (s *Store) ListCapcodeSuggestions(ctx context.Context, p ListParams) (*PaginatedResult, error) {
	return s.paginatedQuery(ctx,
		`capcode_suggestions cs
		 LEFT JOIN users u ON u.id = cs.user_id
		 LEFT JOIN capcodes c ON c.id = cs.capcode_id
		 LEFT JOIN agencies a ON a.id = c.agency_id`,
		`cs.*, u.name as user_name, c.alias as current_alias, a.agency as current_agency`,
		"1=1", nil, p, []string{"cs.address", "cs.agency", "cs.alias", "u.name"})
}

func (s *Store) GetCapcodeSuggestion(ctx context.Context, id int64) (map[string]any, error) {
	return s.getRow(ctx, "capcode_suggestions", id, "")
}

func (s *Store) ApproveCapcodeSuggestion(ctx context.Context, id int64) (int64, error) {
	suggestion, err := s.GetCapcodeSuggestion(ctx, id)
	if err != nil {
		return 0, err
	}

	address, _ := suggestion["address"].(string)
	alias, _ := suggestion["alias"].(string)
	agencyName, _ := suggestion["agency"].(string)
	icon, _ := suggestion["icon"].(string)
	colour, _ := suggestion["colour"].(string)

	// Resolve / upsert the agency only if a name was provided. Empty agency
	// is valid for an amendment that only touches the alias.
	//
	// We don't use ON CONFLICT (agency) here because that requires a unique
	// constraint or index on the column — migration 019 adds it, but older
	// installations may not have run it yet. SELECT-then-INSERT covers both.
	// The race window (two concurrent approvals of the same agency) is
	// vanishingly small and would just produce a second row that the
	// unique index will block once it lands.
	var agencyID *int64
	if agencyName != "" {
		var aid int64
		err = s.db.QueryRowContext(ctx,
			`SELECT id FROM agencies WHERE agency = $1`, agencyName).Scan(&aid)
		if err == sql.ErrNoRows {
			err = s.db.QueryRowContext(ctx,
				`INSERT INTO agencies (agency, created_at, updated_at) VALUES ($1, NOW(), NOW()) RETURNING id`,
				agencyName).Scan(&aid)
		}
		if err != nil {
			return 0, fmt.Errorf("resolving agency %q: %w", agencyName, err)
		}
		agencyID = &aid
	}

	// If the suggestion targets an existing capcode (an amendment), UPDATE.
	// Otherwise INSERT a new one.
	var existingID int64
	if v, ok := suggestion["capcode_id"]; ok && v != nil {
		switch t := v.(type) {
		case int64:
			existingID = t
		case int:
			existingID = int64(t)
		case float64:
			existingID = int64(t)
		}
	}

	var capcodeID int64
	if existingID > 0 {
		// Amendment — only overwrite fields the user actually supplied. The
		// COALESCE with NULLIF keeps unchanged columns intact.
		err = s.db.QueryRowContext(ctx, `
			UPDATE capcodes
			   SET alias     = COALESCE(NULLIF($2, ''), alias),
			       agency_id = COALESCE($3, agency_id),
			       icon      = COALESCE(NULLIF($4, ''), icon),
			       colour    = COALESCE(NULLIF($5, ''), colour),
			       updated_at = NOW()
			 WHERE id = $1
			 RETURNING id`,
			existingID, alias, agencyID, icon, colour).Scan(&capcodeID)
		if err != nil {
			return 0, err
		}
	} else {
		// New capcode — agency is required.
		if agencyID == nil {
			return 0, fmt.Errorf("agency is required when creating a new capcode")
		}
		if icon == "" {
			icon = "fas fa-broadcast-tower"
		}
		if colour == "" {
			colour = "#3490dc"
		}
		err = s.db.QueryRowContext(ctx, `
			INSERT INTO capcodes (address, alias, agency_id, icon, colour, "ignore", created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, false, NOW(), NOW())
			RETURNING id`,
			address, alias, *agencyID, icon, colour).Scan(&capcodeID)
		if err != nil {
			return 0, err
		}
	}

	// Backfill historical messages that were ingested with this address
	// before the capcode existed. New incoming messages already get
	// capcode_id set at ingest time, but anything that came in while the
	// address was unknown is sitting with capcode_id IS NULL — claim them.
	//
	// For an amendment the existing capcode already owns its messages, but
	// there can still be stragglers (e.g. messages stored during a brief
	// window when the lookup race lost). The predicate `capcode_id IS NULL`
	// makes this safe to run unconditionally.
	if capcodeID > 0 && address != "" {
		res, err := s.db.ExecContext(ctx,
			`UPDATE messages
			    SET capcode_id = $1, updated_at = NOW()
			  WHERE address = $2
			    AND capcode_id IS NULL
			    AND deleted_at IS NULL`,
			capcodeID, address)
		if err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				slog.Info("backfilled messages on capcode approval",
					"capcode_id", capcodeID, "address", address, "rows", n)
			}
		} else {
			slog.Warn("backfill messages failed",
				"capcode_id", capcodeID, "address", address, "error", err)
		}
	}

	_, _ = s.db.ExecContext(ctx, `DELETE FROM capcode_suggestions WHERE id = $1`, id)
	return capcodeID, nil
}

func (s *Store) DeleteCapcodeSuggestion(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM capcode_suggestions WHERE id = $1`, id)
	return err
}

// --- Major Incidents ---

func (s *Store) ListMajorIncidents(ctx context.Context, p ListParams) (*PaginatedResult, error) {
	return s.paginatedQuery(ctx, "majorincidents", "*",
		"1=1", nil, p, []string{"title", "category"})
}

func (s *Store) GetMajorIncident(ctx context.Context, id int64) (map[string]any, error) {
	return s.getRow(ctx, "majorincidents", id, "")
}

func (s *Store) CreateMajorIncident(ctx context.Context, data map[string]any) (int64, error) {
	return s.insertRow(ctx, "majorincidents", data)
}

func (s *Store) UpdateMajorIncident(ctx context.Context, id int64, data map[string]any) error {
	return s.updateRow(ctx, "majorincidents", id, data)
}

func (s *Store) DeleteMajorIncident(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM majorincidents WHERE id = $1`, id)
	return err
}

// --- Hazard Reductions ---

func (s *Store) ListHazardReductions(ctx context.Context, p ListParams) (*PaginatedResult, error) {
	return s.paginatedQuery(ctx, "hazardreductions", "*",
		"1=1", nil, p, []string{"title", "\"leadAgency\"", "lga"})
}

func (s *Store) GetHazardReduction(ctx context.Context, id int64) (map[string]any, error) {
	return s.getRow(ctx, "hazardreductions", id, "")
}

func (s *Store) CreateHazardReduction(ctx context.Context, data map[string]any) (int64, error) {
	return s.insertRow(ctx, "hazardreductions", data)
}

func (s *Store) UpdateHazardReduction(ctx context.Context, id int64, data map[string]any) error {
	return s.updateRow(ctx, "hazardreductions", id, data)
}

func (s *Store) DeleteHazardReduction(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM hazardreductions WHERE id = $1`, id)
	return err
}

// --- Dashboard Stats ---

type DashboardStats struct {
	TotalUsers         int64 `json:"total_users" db:"total_users"`
	PaidUsers          int64 `json:"paid_users" db:"paid_users"`
	FreeUsers          int64 `json:"free_users" db:"free_users"`
	TotalCapcodes      int64 `json:"total_capcodes" db:"total_capcodes"`
	TotalMessages      int64 `json:"total_messages" db:"total_messages"`
	TotalSystems       int64 `json:"total_systems" db:"total_systems"`
	MessagesToday      int64 `json:"messages_today" db:"messages_today"`
	NewUsersToday      int64 `json:"new_users_today" db:"new_users_today"`
	TotalInstallations int64 `json:"total_installations" db:"total_installations"`
}

func (s *Store) GetDashboardStats(ctx context.Context) (*DashboardStats, error) {
	val, err := s.cachedStats("dashboard", 15*time.Second, func() (any, error) {
		var stats DashboardStats
		// total_messages uses pg_class.reltuples (autovacuum-maintained estimate)
		// instead of COUNT(*) — exact count over a 2M+ row table costs hundreds
		// of ms even with the partial index. The headline number doesn't need
		// to be precise.
		err := s.db.GetContext(ctx, &stats, `
			SELECT
				(SELECT COUNT(*) FROM users) as total_users,
				(SELECT COUNT(*) FROM users WHERE subscription_valid = true) as paid_users,
				(SELECT COUNT(*) FROM users WHERE subscription_valid = false OR subscription_valid IS NULL) as free_users,
				(SELECT COUNT(*) FROM capcodes WHERE deleted_at IS NULL) as total_capcodes,
				(SELECT COALESCE(reltuples, 0)::bigint FROM pg_class WHERE relname = 'messages') as total_messages,
				(SELECT COUNT(*) FROM systems WHERE deleted_at IS NULL) as total_systems,
				(SELECT COUNT(*) FROM messages WHERE deleted_at IS NULL AND created_at >= CURRENT_DATE) as messages_today,
				(SELECT COUNT(*) FROM users WHERE created_at >= CURRENT_DATE) as new_users_today,
				(SELECT COUNT(*) FROM installations WHERE deleted_at IS NULL) as total_installations
		`)
		if err != nil {
			return nil, err
		}
		return &stats, nil
	})
	if err != nil {
		return nil, err
	}
	return val.(*DashboardStats), nil
}

// --- App versions ---

// NewAppMajor is the first major version of the rebuilt (Go-backed) app.
// Installs on it or later count as "on the new app".
const NewAppMajor = 5

// appVersionWindow is how recently an installation must have checked in
// (installations.updated_at) to count as active.
const appVersionWindow = 30 * 24 * time.Hour

type AppVersionRow struct {
	Version  string `json:"version" db:"version"`
	IOS      int64  `json:"ios" db:"ios"`
	Android  int64  `json:"android" db:"android"`
	Devices  int64  `json:"devices" db:"devices"`
	Accounts int64  `json:"accounts" db:"accounts"`
}

type AppVersionStats struct {
	WindowDays        int             `json:"window_days"`
	NewAppMajor       int             `json:"new_app_major"`
	ActiveDevices     int64           `json:"active_devices" db:"active_devices"`
	NewAppDevices     int64           `json:"new_app_devices" db:"new_app_devices"`
	ActiveAccounts    int64           `json:"active_accounts" db:"active_accounts"`
	NewAppAccounts    int64           `json:"new_app_accounts" db:"new_app_accounts"`
	RegisteredDevices int64           `json:"registered_devices" db:"registered_devices"`
	Versions          []AppVersionRow `json:"versions"`
}

// GetAppVersionStats reports which app versions active installations run,
// split by platform, and how many have moved to the new app. Cached for 60s.
func (s *Store) GetAppVersionStats(ctx context.Context) (*AppVersionStats, error) {
	val, err := s.cachedStats("app_versions", 60*time.Second, func() (any, error) {
		stats := AppVersionStats{WindowDays: int(appVersionWindow.Hours() / 24), NewAppMajor: NewAppMajor}
		secs := appVersionWindow.Seconds()
		// The CASE guards the cast: app_version is free text from the client.
		err := s.db.GetContext(ctx, &stats, `
			WITH active AS (
				SELECT user_id,
				       COALESCE(CASE WHEN split_part(app_version, '.', 1) ~ '^[0-9]{1,6}$'
				                     THEN split_part(app_version, '.', 1)::int END, 0) >= $2 AS new_app
				  FROM installations
				 WHERE deleted_at IS NULL AND updated_at > NOW() - make_interval(secs => $1)
			)
			SELECT COUNT(*) AS active_devices,
			       COUNT(*) FILTER (WHERE new_app) AS new_app_devices,
			       COUNT(DISTINCT user_id) AS active_accounts,
			       COUNT(DISTINCT user_id) FILTER (WHERE new_app) AS new_app_accounts,
			       (SELECT COUNT(*) FROM installations WHERE deleted_at IS NULL) AS registered_devices
			  FROM active`, secs, NewAppMajor)
		if err != nil {
			return nil, err
		}
		stats.Versions = []AppVersionRow{}
		err = s.db.SelectContext(ctx, &stats.Versions, `
			SELECT COALESCE(NULLIF(app_version, ''), 'unknown') AS version,
			       COUNT(*) FILTER (WHERE push_type = 'APN') AS ios,
			       COUNT(*) FILTER (WHERE push_type = 'GCM') AS android,
			       COUNT(*) AS devices,
			       COUNT(DISTINCT user_id) AS accounts
			  FROM installations
			 WHERE deleted_at IS NULL AND updated_at > NOW() - make_interval(secs => $1)
			 GROUP BY 1`, secs)
		if err != nil {
			return nil, err
		}
		sort.Slice(stats.Versions, func(i, j int) bool {
			return compareVersions(stats.Versions[i].Version, stats.Versions[j].Version) > 0
		})
		return &stats, nil
	})
	if err != nil {
		return nil, err
	}
	return val.(*AppVersionStats), nil
}

// compareVersions orders dotted version strings numerically ("5.10.0" after
// "5.9.1"); non-numeric parts sort below numeric ones.
func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		na, nb := -1, -1
		if i < len(pa) {
			if n, err := strconv.Atoi(pa[i]); err == nil {
				na = n
			}
		}
		if i < len(pb) {
			if n, err := strconv.Atoi(pb[i]); err == nil {
				nb = n
			}
		}
		if na != nb {
			if na < nb {
				return -1
			}
			return 1
		}
	}
	return strings.Compare(a, b)
}

// --- Billing / IAP ---

// BillingStats is the headline IAP/Stripe billing snapshot for the dashboard.
type BillingStats struct {
	ActiveTotal  int64 `json:"active_total" db:"active_total"`
	ActiveApple  int64 `json:"active_apple" db:"active_apple"`
	ActiveGoogle int64 `json:"active_google" db:"active_google"`
	ActiveStripe int64 `json:"active_stripe" db:"active_stripe"`
	ActiveGifted int64 `json:"active_gifted" db:"active_gifted"`
	// ActiveHostComp counts running receiver-host comps. They are not store
	// subscriptions, so the other active_* counts and MRR leave them out.
	ActiveHostComp   int64 `json:"active_host_comp" db:"active_host_comp"`
	NewSubs7d        int64 `json:"new_subs_7d" db:"new_subs_7d"`
	NewSubs30d       int64 `json:"new_subs_30d" db:"new_subs_30d"`
	Cancellations30d int64 `json:"cancellations_30d" db:"cancellations_30d"`
	Refunds30d       int64 `json:"refunds_30d" db:"refunds_30d"`
	ExpiringSoon     int64 `json:"expiring_soon" db:"expiring_soon"`
	WillNotRenew     int64 `json:"will_not_renew" db:"will_not_renew"`
	MRREstimateCents int64 `json:"mrr_estimate_cents" db:"mrr_estimate_cents"`
}

// GetBillingStats computes a snapshot of active subscriptions, recent churn,
// and an MRR estimate. MRR is a rough approximation: monthly plans count at
// their price, yearly plans amortised to 1/12. Cached for 15s.
func (s *Store) GetBillingStats(ctx context.Context) (*BillingStats, error) {
	val, err := s.cachedStats("billing", 15*time.Second, func() (any, error) {
		stats, err := s.fetchBillingStats(ctx)
		if err != nil {
			return nil, err
		}
		return stats, nil
	})
	if err != nil {
		return nil, err
	}
	return val.(*BillingStats), nil
}

// Store prices in cents, used only for the MRR estimate. These are the list
// prices in AUD and must match apps/web/.../subscribe.tsx. The estimate is
// gross: it does not deduct the 15-30% store commission, and it is in AUD, so
// it will read higher than a provider dashboard reporting net USD.
//
// MRR amortises a yearly plan over twelve months. A previous version used
// 8333 cents for the yearly row — a tenth-of-a-dollar slip that reported the
// yearly plan as $83.33/month and inflated MRR roughly tenfold.
//
// The estimate keys off subscription_valid alone, deliberately. That flag is
// reconciled against the store (see cmd/adapty-backfill), and it stays true
// for users in a billing grace period whose expiry has already passed — they
// are still paying customers, so filtering on expiry here would undercount.
const (
	priceMonthlyCents = 499  // $4.99/month
	priceYearlyCents  = 2999 // $29.99/year
)

func (s *Store) fetchBillingStats(ctx context.Context) (*BillingStats, error) {
	var stats BillingStats
	err := s.db.GetContext(ctx, &stats, `
		SELECT
			(SELECT COUNT(*) FROM users WHERE subscription_valid = true) AS active_total,
			(SELECT COUNT(*) FROM users WHERE subscription_valid = true AND subscription_platform = 'apple') AS active_apple,
			(SELECT COUNT(*) FROM users WHERE subscription_valid = true AND subscription_platform = 'google') AS active_google,
			(SELECT COUNT(*) FROM users WHERE subscription_valid = true AND subscription_platform = 'stripe') AS active_stripe,
			(SELECT COUNT(*) FROM users WHERE subscription_valid = true AND subscription_product_id LIKE '%gifted%') AS active_gifted,
			(SELECT COUNT(*) FROM users WHERE host_comp_until > NOW()) AS active_host_comp,
			(SELECT COUNT(DISTINCT user_id) FROM iap_transactions
				WHERE event_type = 'purchase' AND is_valid = true AND created_at >= NOW() - INTERVAL '7 days') AS new_subs_7d,
			(SELECT COUNT(DISTINCT user_id) FROM iap_transactions
				WHERE event_type = 'purchase' AND is_valid = true AND created_at >= NOW() - INTERVAL '30 days') AS new_subs_30d,
			(SELECT COUNT(*) FROM iap_transactions
				WHERE event_type IN ('webhook:EXPIRED','webhook:GRACE_PERIOD_EXPIRED','webhook:REVOKE','webhook:DID_CHANGE_RENEWAL_STATUS')
				AND created_at >= NOW() - INTERVAL '30 days') AS cancellations_30d,
			(SELECT COUNT(*) FROM iap_transactions
				WHERE event_type = 'webhook:REFUND' AND created_at >= NOW() - INTERVAL '30 days') AS refunds_30d,
			(SELECT COUNT(*) FROM users
				WHERE subscription_valid = true AND subscription_expiry IS NOT NULL
				AND subscription_expiry BETWEEN NOW() AND NOW() + INTERVAL '7 days') AS expiring_soon,
			(SELECT COUNT(*) FROM users WHERE subscription_valid = true AND will_renew = false) AS will_not_renew,
			(SELECT COALESCE(SUM(
				CASE
					WHEN subscription_product_id ILIKE '%gifted%'  THEN 0
					WHEN subscription_product_id ILIKE '%yearly%'  THEN $1
					WHEN subscription_product_id ILIKE '%monthly%' THEN $2
					ELSE 0
				END), 0)::bigint
				FROM users
				WHERE subscription_valid = true) AS mrr_estimate_cents
	`, priceYearlyCents/12, priceMonthlyCents)
	return &stats, err
}

// ListIAPTransactions returns a paginated, filterable view of the iap_transactions log.
// Supports filtering by platform, event_type, and user_id via query params.
func (s *Store) ListIAPTransactions(ctx context.Context, p ListParams, platform, eventType string, userID int64) (*PaginatedResult, error) {
	where := "1=1"
	var args []any
	argIdx := 1
	if platform != "" {
		where += fmt.Sprintf(" AND t.platform = $%d", argIdx)
		args = append(args, platform)
		argIdx++
	}
	if eventType != "" {
		where += fmt.Sprintf(" AND t.event_type = $%d", argIdx)
		args = append(args, eventType)
		argIdx++
	}
	if userID > 0 {
		where += fmt.Sprintf(" AND t.user_id = $%d", argIdx)
		args = append(args, userID)
	}

	// Default sort to most recent first.
	if p.SortBy == "" {
		p.SortBy = "t.created_at"
		p.SortDir = "desc"
	}

	return s.paginatedQuery(ctx,
		"iap_transactions t LEFT JOIN users u ON u.id = t.user_id",
		`t.id, t.user_id, t.platform, t.transaction_id, t.original_transaction_id,
		 t.product_id, t.event_type, t.is_valid, t.expires_at, t.will_renew,
		 t.environment, t.created_at,
		 u.name AS user_name, u.email AS user_email`,
		where, args, p,
		[]string{"u.name", "u.email", "t.transaction_id", "t.original_transaction_id", "t.product_id"},
		"iap_transactions t")
}

// GetIAPTransaction returns one transaction with raw_payload included.
func (s *Store) GetIAPTransaction(ctx context.Context, id int64) (map[string]any, error) {
	row := s.db.QueryRowxContext(ctx,
		`SELECT t.*, u.name AS user_name, u.email AS user_email
		 FROM iap_transactions t
		 LEFT JOIN users u ON u.id = t.user_id
		 WHERE t.id = $1`, id)
	result := make(map[string]any)
	if err := row.MapScan(result); err != nil {
		return nil, err
	}
	cleanMapScan(result)
	return result, nil
}

// ListUserInstallations returns a user's devices for the user detail page.
func (s *Store) ListUserInstallations(ctx context.Context, userID int64) ([]map[string]any, error) {
	rows, err := s.db.QueryxContext(ctx,
		`SELECT id, vendor, model, version, app_version, push_type, timezone, created_at, updated_at, deleted_at
		 FROM installations
		 WHERE user_id = $1
		 ORDER BY (deleted_at IS NULL) DESC, updated_at DESC NULLS LAST, created_at DESC`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []map[string]any
	for rows.Next() {
		m := make(map[string]any)
		if err := rows.MapScan(m); err != nil {
			return nil, err
		}
		cleanMapScan(m)
		out = append(out, m)
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, nil
}

// --- Generic helpers ---

type ListParams struct {
	Page           int
	PerPage        int
	Search         string
	SortBy         string
	SortDir        string
	IncludeDeleted bool // opt-in: include soft-deleted rows in the result set
}

type PaginatedResult struct {
	Data       []map[string]any `json:"data"`
	Total      int64            `json:"total"`
	Page       int              `json:"page"`
	PerPage    int              `json:"per_page"`
	TotalPages int              `json:"total_pages"`
}

// allowedSortColumns is a safelist check to prevent SQL injection in ORDER BY.
var allowedSortDirs = map[string]bool{"asc": true, "desc": true, "ASC": true, "DESC": true}

// approxReltuplesPrefix marks a countFrom value as a request to use pg_class.reltuples
// (autovacuum-maintained row estimate) instead of an exact COUNT(*). Used for large
// tables where exact counts are prohibitively expensive.
const approxReltuplesPrefix = "__approx_reltuples__:"

// approxCountReltuples returns a sentinel countFrom value that tells paginatedQuery
// to use pg_class.reltuples for the row count instead of COUNT(*).
func approxCountReltuples(table string) string { return approxReltuplesPrefix + table }

func (s *Store) paginatedQuery(ctx context.Context, from, selectCols, where string, whereArgs []any, p ListParams, searchCols []string, countFrom ...string) (*PaginatedResult, error) {
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PerPage < 1 {
		p.PerPage = 25
	}
	if p.PerPage > 100 {
		p.PerPage = 100
	}

	args := make([]any, len(whereArgs))
	copy(args, whereArgs)
	argIdx := len(args) + 1

	// Search
	if p.Search != "" && len(searchCols) > 0 {
		clauses := make([]string, len(searchCols))
		for i, col := range searchCols {
			clauses[i] = fmt.Sprintf("CAST(%s AS TEXT) ILIKE $%d", col, argIdx)
		}
		where += fmt.Sprintf(" AND (%s)", strings.Join(clauses, " OR "))
		args = append(args, "%"+p.Search+"%")
		argIdx++
	}

	// Count - use simpler table for count when JOINs aren't needed for filtering.
	// When countFrom is an approx-reltuples sentinel and there's no search, use the
	// autovacuum-maintained row estimate instead of scanning the full index (huge
	// win on tables like messages with 2M+ rows where COUNT(*) takes ~300ms).
	var total int64
	if len(countFrom) > 0 && countFrom[0] != "" && p.Search == "" && strings.HasPrefix(countFrom[0], approxReltuplesPrefix) {
		table := strings.TrimPrefix(countFrom[0], approxReltuplesPrefix)
		if err := s.db.GetContext(ctx, &total,
			`SELECT COALESCE(reltuples, 0)::bigint FROM pg_class WHERE relname = $1`, table); err != nil {
			return nil, fmt.Errorf("count query: %w", err)
		}
	} else {
		countTable := from
		if len(countFrom) > 0 && countFrom[0] != "" && p.Search == "" {
			countTable = countFrom[0]
		}
		countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s", countTable, where)
		if err := s.db.GetContext(ctx, &total, countQuery, args...); err != nil {
			return nil, fmt.Errorf("count query: %w", err)
		}
	}

	// Sort - validate to prevent injection
	orderClause := "ORDER BY id DESC"
	if p.SortBy != "" {
		dir := "ASC"
		if allowedSortDirs[p.SortDir] {
			dir = strings.ToUpper(p.SortDir)
		}
		// Only allow alphanumeric, underscores, and dots in sort column
		safeSortBy := sanitizeColumnName(p.SortBy)
		if safeSortBy != "" {
			orderClause = fmt.Sprintf("ORDER BY %s %s", safeSortBy, dir)
		}
	}

	offset := (p.Page - 1) * p.PerPage
	if offset < 0 {
		// A page near math.MaxInt64 overflows the int64 product and wraps
		// negative; Postgres rejects a negative OFFSET (2201X).
		offset = 0
	}
	dataQuery := fmt.Sprintf("SELECT %s FROM %s WHERE %s %s LIMIT %d OFFSET %d",
		selectCols, from, where, orderClause, p.PerPage, offset)

	rows, err := s.db.QueryxContext(ctx, dataQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("data query: %w", err)
	}
	defer rows.Close()

	var data []map[string]any
	for rows.Next() {
		row := make(map[string]any)
		if err := rows.MapScan(row); err != nil {
			return nil, err
		}
		cleanMapScan(row)
		data = append(data, row)
	}
	if data == nil {
		data = []map[string]any{}
	}

	return &PaginatedResult{
		Data:       data,
		Total:      total,
		Page:       p.Page,
		PerPage:    p.PerPage,
		TotalPages: int(math.Ceil(float64(total) / float64(p.PerPage))),
	}, nil
}

func (s *Store) getRow(ctx context.Context, from string, id int64, selectCols string) (map[string]any, error) {
	if selectCols == "" {
		selectCols = "*"
	}
	// Determine the ID column based on table alias
	idCol := "id"
	parts := strings.Fields(from)
	if len(parts) > 1 {
		idCol = parts[1] + ".id"
	}

	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = $1", selectCols, from, idCol)
	row := s.db.QueryRowxContext(ctx, query, id)
	result := make(map[string]any)
	if err := row.MapScan(result); err != nil {
		return nil, err
	}
	cleanMapScan(result)
	return result, nil
}

func (s *Store) insertRow(ctx context.Context, table string, data map[string]any) (int64, error) {
	if err := validateWritableFields(table, data); err != nil {
		return 0, err
	}
	data["created_at"] = time.Now()
	data["updated_at"] = time.Now()

	cols := make([]string, 0, len(data))
	placeholders := make([]string, 0, len(data))
	vals := make([]any, 0, len(data))
	i := 1
	for k, v := range data {
		cols = append(cols, fmt.Sprintf(`"%s"`, k))
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		vals = append(vals, v)
		i++
	}

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING id",
		table, strings.Join(cols, ", "), strings.Join(placeholders, ", "))

	var id int64
	err := s.db.QueryRowContext(ctx, query, vals...).Scan(&id)
	return id, err
}

func (s *Store) updateRow(ctx context.Context, table string, id int64, data map[string]any) error {
	if err := validateWritableFields(table, data); err != nil {
		return err
	}
	data["updated_at"] = time.Now()

	sets := make([]string, 0, len(data))
	vals := make([]any, 0, len(data)+1)
	i := 1
	for k, v := range data {
		sets = append(sets, fmt.Sprintf(`"%s" = $%d`, k, i))
		vals = append(vals, v)
		i++
	}
	vals = append(vals, id)

	query := fmt.Sprintf("UPDATE %s SET %s WHERE id = $%d", table, strings.Join(sets, ", "), i)
	_, err := s.db.ExecContext(ctx, query, vals...)
	return err
}

var adminUserInputFields = fieldSet("email", "name", "password", "role", "active")

// writableFields prevents untrusted JSON keys from becoming SQL identifiers.
// Timestamps are assigned internally after this validation.
var writableFields = map[string]map[string]struct{}{
	"admin_users":      fieldSet("email", "name", "role", "active", "password_hash"),
	"users":            fieldSet("user_id", "name", "email", "password", "email_verified_at", "adapty_id", "last_adapty_sync_at", "last_store_sync_at", "subscription_product_id", "subscription_platform", "subscription_expiry", "subscription_valid", "will_renew", "cancellation_reason", "apple_original_transaction_id", "google_purchase_token", "superuser"),
	"capcodes":         fieldSet("address", "agency_id", "alias", "icon", "colour", "ignore"),
	"agencies":         fieldSet("agency", "coverage_district"),
	"systems":          fieldSet("user_id", "system_id", "name", "api_key", "sync", "steal_capcodes", "frequency", "base_url", "lat", "lng", "located_since", "suburb", "request_notes", "hardware"),
	"messages":         fieldSet("system_id", "capcode_id", "address", "message", "lat", "lng", "created_at"),
	"subscriptions":    fieldSet("user_id", "capcode_id"),
	"alertzones":       fieldSet("user_id", "name", "lat", "lng", "radius"),
	"majorincidents":   fieldSet("title", "upstream_id", "category", "description", "lat", "lng", "start_at", "end_at", "geometry", "data"),
	"hazardreductions": fieldSet("title", "upstream_id", "leadAgency", "supportingAgencies", "lga", "size", "tenure", "start_at", "end_at"),
}

func fieldSet(fields ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		set[field] = struct{}{}
	}
	return set
}

func validateWritableFields(table string, data map[string]any) error {
	allowed, ok := writableFields[table]
	if !ok {
		return fmt.Errorf("unsupported writable table %q", table)
	}
	return validateFields(data, allowed)
}

func validateFields(data map[string]any, allowed map[string]struct{}) error {
	for field := range data {
		if _, ok := allowed[field]; !ok {
			return fmt.Errorf("field %q is not writable", field)
		}
	}
	return nil
}

// sanitizeColumnName allows only safe characters for ORDER BY columns.
func sanitizeColumnName(name string) string {
	for _, ch := range name {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '.') {
			return ""
		}
	}
	return name
}

// cleanMapScan converts []byte values to strings and handles SQL null types for JSON serialization.
func cleanMapScan(m map[string]any) {
	for k, v := range m {
		if sensitiveMapField(k) {
			delete(m, k)
			continue
		}
		switch val := v.(type) {
		case []byte:
			// Try to parse as JSON first
			var jsonVal any
			if err := json.Unmarshal(val, &jsonVal); err == nil {
				m[k] = jsonVal
			} else {
				m[k] = string(val)
			}
		case sql.NullString:
			if val.Valid {
				m[k] = val.String
			} else {
				m[k] = nil
			}
		case sql.NullInt64:
			if val.Valid {
				m[k] = val.Int64
			} else {
				m[k] = nil
			}
		case sql.NullFloat64:
			if val.Valid {
				m[k] = val.Float64
			} else {
				m[k] = nil
			}
		case sql.NullBool:
			if val.Valid {
				m[k] = val.Bool
			} else {
				m[k] = nil
			}
		case sql.NullTime:
			if val.Valid {
				m[k] = val.Time
			} else {
				m[k] = nil
			}
		}
	}
}

func sensitiveMapField(field string) bool {
	switch strings.ToLower(field) {
	case "password", "password_hash", "remember_token", "two_factor_secret", "two_factor_recovery_codes", "token_version":
		return true
	default:
		return false
	}
}
