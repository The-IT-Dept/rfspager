package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Receiver remote management: config revisions, stats, commands and captures.
// Schema in migrations/032_receiver_remote_management.sql; the wire types the
// JSON columns hold live in internal/rxproto.

// Command statuses, in lifecycle order.
const (
	CommandQueued   = "queued"
	CommandSent     = "sent"
	CommandProgress = "progress"
	CommandOK       = "ok"
	CommandError    = "error"
	CommandExpired  = "expired"
)

// Capture statuses.
const (
	CaptureRequested = "requested"
	CaptureRecording = "recording"
	CaptureReady     = "ready"
	CaptureError     = "error"
)

// ErrNotFound is returned by the Get* helpers below when no row matches.
var ErrNotFound = errors.New("not found")

// ReceiverConfigRevision is one row of receiver_config_revisions. Config is
// stored as written; readers normalise it over rxproto.Defaults().
type ReceiverConfigRevision struct {
	ID        int64           `db:"id" json:"id"`
	SystemID  int64           `db:"system_id" json:"system_id"`
	Version   int             `db:"version" json:"version"`
	Config    json.RawMessage `db:"config" json:"config"`
	Source    string          `db:"source" json:"source"`
	Note      *string         `db:"note" json:"note"`
	CreatedAt time.Time       `db:"created_at" json:"created_at"`
}

// SystemStatsRow is one sampled stats report.
type SystemStatsRow struct {
	TS    time.Time       `db:"ts" json:"ts"`
	Stats json.RawMessage `db:"stats" json:"stats"`
}

// SystemCommand is one row of system_commands.
type SystemCommand struct {
	ID          string          `db:"id" json:"id"`
	SystemID    int64           `db:"system_id" json:"system_id"`
	Type        string          `db:"type" json:"type"`
	Params      json.RawMessage `db:"params" json:"params"`
	RequestedBy string          `db:"requested_by" json:"requested_by"`
	Status      string          `db:"status" json:"status"`
	Stage       *string         `db:"stage" json:"stage"`
	Message     *string         `db:"message" json:"message"`
	CreatedAt   time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time       `db:"updated_at" json:"updated_at"`
}

// SystemCapture is one row of system_captures. Path is where the server
// stored the upload and is deliberately not serialised; downloads go through
// the admin API.
type SystemCapture struct {
	ID          string     `db:"id" json:"id"`
	SystemID    int64      `db:"system_id" json:"system_id"`
	CommandID   *string    `db:"command_id" json:"command_id"`
	Kind        string     `db:"kind" json:"kind"`
	Channel     *string    `db:"channel" json:"channel"`
	Seconds     *int       `db:"seconds" json:"seconds"`
	SampleRate  *int       `db:"sample_rate" json:"sample_rate"`
	Status      string     `db:"status" json:"status"`
	Path        *string    `db:"path" json:"-"`
	SizeBytes   *int64     `db:"size_bytes" json:"size_bytes"`
	Error       *string    `db:"error" json:"error"`
	RequestedBy string     `db:"requested_by" json:"requested_by"`
	CreatedAt   time.Time  `db:"created_at" json:"created_at"`
	ReadyAt     *time.Time `db:"ready_at" json:"ready_at"`
}

// --- config revisions ---

// CurrentReceiverConfig returns the newest revision's version and raw JSON
// for a system. found is false when the system has no revisions yet, in
// which case the receiver runs rxproto.Defaults().
func (d *DB) CurrentReceiverConfig(ctx context.Context, systemID int64) (version int, raw []byte, found bool, err error) {
	var rev ReceiverConfigRevision
	err = d.conn.GetContext(ctx, &rev,
		`SELECT version, config FROM receiver_config_revisions
		  WHERE system_id = $1 ORDER BY version DESC LIMIT 1`, systemID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, false, nil
	}
	if err != nil {
		return 0, nil, false, err
	}
	return rev.Version, rev.Config, true, nil
}

// GetReceiverConfigRevision loads one revision by version.
func (d *DB) GetReceiverConfigRevision(ctx context.Context, systemID int64, version int) (*ReceiverConfigRevision, error) {
	var rev ReceiverConfigRevision
	err := d.conn.GetContext(ctx, &rev,
		`SELECT * FROM receiver_config_revisions WHERE system_id = $1 AND version = $2`, systemID, version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rev, nil
}

// lockReceiverConfig serialises revision writers for one system within the
// calling transaction. Keyed through hashtextextended so it cannot collide
// with the message dedupe lock, which hashes pager addresses.
func lockReceiverConfig(ctx context.Context, tx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, systemID int64) error {
	_, err := tx.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended('receiver_config:' || $1::text, 0))`, systemID)
	if err != nil {
		return fmt.Errorf("locking receiver config: %w", err)
	}
	return nil
}

// InsertReceiverConfigRevision appends a revision numbered one above the
// system's current maximum and returns that number. The advisory lock makes
// the max+1 safe against two admins (or an admin and a seeding receiver)
// writing at once; without it both would compute the same version and one
// would fail the unique constraint.
func (d *DB) InsertReceiverConfigRevision(ctx context.Context, systemID int64, raw []byte, source, note string) (int, error) {
	tx, err := d.conn.BeginTxx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockReceiverConfig(ctx, tx, systemID); err != nil {
		return 0, err
	}
	var version int
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO receiver_config_revisions (system_id, version, config, source, note)
		 SELECT $1, COALESCE(MAX(version), 0) + 1, $2::jsonb, $3, NULLIF($4, '')
		   FROM receiver_config_revisions WHERE system_id = $1
		 RETURNING version`,
		systemID, raw, source, note).Scan(&version); err != nil {
		return 0, err
	}
	return version, tx.Commit()
}

// SeedReceiverConfigRevision inserts version 1 only when the system has no
// revisions, and returns whichever revision is current afterwards. seeded
// reports whether this call wrote it. Used when a proto-2 receiver connects
// and the server has never stored a config for its system.
func (d *DB) SeedReceiverConfigRevision(ctx context.Context, systemID int64, raw []byte, source, note string) (rev *ReceiverConfigRevision, seeded bool, err error) {
	tx, err := d.conn.BeginTxx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockReceiverConfig(ctx, tx, systemID); err != nil {
		return nil, false, err
	}
	var current ReceiverConfigRevision
	err = tx.GetContext(ctx, &current,
		`SELECT * FROM receiver_config_revisions WHERE system_id = $1 ORDER BY version DESC LIMIT 1`, systemID)
	switch {
	case err == nil:
		return &current, false, tx.Commit()
	case !errors.Is(err, sql.ErrNoRows):
		return nil, false, err
	}
	if err := tx.GetContext(ctx, &current,
		`INSERT INTO receiver_config_revisions (system_id, version, config, source, note)
		 VALUES ($1, 1, $2::jsonb, $3, NULLIF($4, ''))
		 RETURNING *`,
		systemID, raw, source, note); err != nil {
		return nil, false, err
	}
	return &current, true, tx.Commit()
}

// ListReceiverConfigRevisions returns the newest `limit` revisions, newest
// first.
func (d *DB) ListReceiverConfigRevisions(ctx context.Context, systemID int64, limit int) ([]ReceiverConfigRevision, error) {
	if limit < 1 || limit > 200 {
		limit = 20
	}
	out := []ReceiverConfigRevision{}
	err := d.conn.SelectContext(ctx, &out,
		`SELECT * FROM receiver_config_revisions
		  WHERE system_id = $1 ORDER BY version DESC LIMIT $2`, systemID, limit)
	return out, err
}

// --- system_connections management columns ---

// UpdateSystemConnectionHello records what the receiver declared in hello:
// protocol version, host facts and the config revision it is running (0 =
// defaults / env bootstrap, stored as NULL). A receiver that reports a
// revision is running it, so config_status starts as applied; the
// config_result for any newer revision the welcome carries overwrites it.
func (d *DB) UpdateSystemConnectionHello(ctx context.Context, systemID int64, podID string, proto int, host []byte, configVersion int) error {
	if proto < 1 {
		proto = 1
	}
	if len(host) == 0 {
		host = nil
	}
	_, err := d.conn.ExecContext(ctx,
		`UPDATE system_connections
		    SET proto = $3,
		        host = $4,
		        config_version = NULLIF($5, 0),
		        config_status = CASE WHEN $5 > 0 THEN 'applied' ELSE NULL END
		  WHERE system_id = $1 AND pod_id = $2`,
		systemID, podID, proto, host, configVersion)
	return err
}

// UpdateSystemConnectionConfigStatus stores the outcome of a config push:
// status is rxproto.ConfigApplied or rxproto.ConfigRejected for version.
func (d *DB) UpdateSystemConnectionConfigStatus(ctx context.Context, systemID int64, version int, status string) error {
	_, err := d.conn.ExecContext(ctx,
		`UPDATE system_connections SET config_version = $2, config_status = $3 WHERE system_id = $1`,
		systemID, version, status)
	return err
}

// --- stats ---

// InsertSystemStats stores one stats report.
func (d *DB) InsertSystemStats(ctx context.Context, systemID int64, ts time.Time, stats []byte) error {
	_, err := d.conn.ExecContext(ctx,
		`INSERT INTO system_stats (system_id, ts, stats) VALUES ($1, $2, $3::jsonb)
		 ON CONFLICT (system_id, ts) DO UPDATE SET stats = EXCLUDED.stats`,
		systemID, ts, stats)
	return err
}

// UpdateSystemConnectionStats caches the newest report on the connection
// row so list views can show health without touching system_stats.
func (d *DB) UpdateSystemConnectionStats(ctx context.Context, systemID int64, stats []byte, at time.Time) error {
	_, err := d.conn.ExecContext(ctx,
		`UPDATE system_connections SET last_stats = $2::jsonb, last_stats_at = $3 WHERE system_id = $1`,
		systemID, stats, at)
	return err
}

// maxStatsRows bounds one sampled query; 7 days at a 1 minute step is 10080
// buckets, so callers wanting a week should ask for a coarser step.
const maxStatsRows = 5000

// QuerySystemStats returns one report per `step` bucket (the earliest in the
// bucket) over the last `since`, oldest first, capped at maxStatsRows.
func (d *DB) QuerySystemStats(ctx context.Context, systemID int64, since, step time.Duration) ([]SystemStatsRow, error) {
	if step < time.Second {
		step = time.Second
	}
	if since < step {
		since = step
	}
	out := []SystemStatsRow{}
	err := d.conn.SelectContext(ctx, &out,
		`SELECT ts, stats FROM (
		     SELECT DISTINCT ON (date_bin($3::interval, ts, TIMESTAMPTZ 'epoch')) ts, stats
		       FROM system_stats
		      WHERE system_id = $1 AND ts >= NOW() - $2::interval
		      ORDER BY date_bin($3::interval, ts, TIMESTAMPTZ 'epoch'), ts
		 ) sampled
		 ORDER BY ts
		 LIMIT $4`,
		systemID, pgInterval(since), pgInterval(step), maxStatsRows)
	return out, err
}

// RecentSystemStats returns the newest `limit` reports, oldest first, for
// backfilling a stats stream subscriber.
func (d *DB) RecentSystemStats(ctx context.Context, systemID int64, limit int) ([]SystemStatsRow, error) {
	if limit < 1 || limit > 1000 {
		limit = 60
	}
	out := []SystemStatsRow{}
	err := d.conn.SelectContext(ctx, &out,
		`SELECT ts, stats FROM (
		     SELECT ts, stats FROM system_stats
		      WHERE system_id = $1 ORDER BY ts DESC LIMIT $2
		 ) newest ORDER BY ts`, systemID, limit)
	return out, err
}

// PruneSystemStats deletes reports older than the cutoff.
func (d *DB) PruneSystemStats(ctx context.Context, olderThan time.Duration) (int64, error) {
	res, err := d.conn.ExecContext(ctx,
		`DELETE FROM system_stats WHERE ts < NOW() - $1::interval`, pgInterval(olderThan))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// --- commands ---

// InsertSystemCommand queues a command. The caller supplies the id (a UUID
// string); status defaults to queued and the insert trigger notifies every
// pod on receiver_command.
func (d *DB) InsertSystemCommand(ctx context.Context, cmd *SystemCommand) error {
	return insertSystemCommand(ctx, d.conn, cmd)
}

type execGetter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	GetContext(context.Context, any, string, ...any) error
}

func insertSystemCommand(ctx context.Context, q execGetter, cmd *SystemCommand) error {
	if len(cmd.Params) == 0 {
		cmd.Params = json.RawMessage(`{}`)
	}
	if cmd.Status == "" {
		cmd.Status = CommandQueued
	}
	return q.GetContext(ctx, cmd,
		`INSERT INTO system_commands (id, system_id, type, params, requested_by, status)
		 VALUES ($1, $2, $3, $4::jsonb, $5, $6)
		 RETURNING *`,
		cmd.ID, cmd.SystemID, cmd.Type, cmd.Params, cmd.RequestedBy, cmd.Status)
}

// InsertSystemCaptureCommand creates a capture row and the command that
// records it in one transaction, so the NOTIFY (delivered at commit) can
// never reach a pod before the capture row exists.
func (d *DB) InsertSystemCaptureCommand(ctx context.Context, capture *SystemCapture, cmd *SystemCommand) error {
	tx, err := d.conn.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := insertSystemCommand(ctx, tx, cmd); err != nil {
		return err
	}
	capture.CommandID = &cmd.ID
	if err := insertSystemCapture(ctx, tx, capture); err != nil {
		return err
	}
	return tx.Commit()
}

// GetSystemCommand loads one command.
func (d *DB) GetSystemCommand(ctx context.Context, id string) (*SystemCommand, error) {
	var cmd SystemCommand
	err := d.conn.GetContext(ctx, &cmd, `SELECT * FROM system_commands WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &cmd, nil
}

// ListSystemCommands returns the newest `limit` commands, newest first.
func (d *DB) ListSystemCommands(ctx context.Context, systemID int64, limit int) ([]SystemCommand, error) {
	if limit < 1 || limit > 500 {
		limit = 50
	}
	out := []SystemCommand{}
	err := d.conn.SelectContext(ctx, &out,
		`SELECT * FROM system_commands WHERE system_id = $1
		  ORDER BY created_at DESC, id DESC LIMIT $2`, systemID, limit)
	return out, err
}

// QueuedSystemCommands returns undelivered commands oldest first, for
// delivery when the receiver connects.
func (d *DB) QueuedSystemCommands(ctx context.Context, systemID int64) ([]SystemCommand, error) {
	out := []SystemCommand{}
	err := d.conn.SelectContext(ctx, &out,
		`SELECT * FROM system_commands WHERE system_id = $1 AND status = $2
		  ORDER BY created_at, id`, systemID, CommandQueued)
	return out, err
}

// ClaimSystemCommand moves a command from queued to sent iff it is still
// queued, and reports whether this caller won. Two pods can be asked to
// deliver the same command (NOTIFY plus connect-time delivery); only the
// winner sends it.
func (d *DB) ClaimSystemCommand(ctx context.Context, id string) (bool, error) {
	res, err := d.conn.ExecContext(ctx,
		`UPDATE system_commands SET status = $2, updated_at = NOW()
		  WHERE id = $1 AND status = $3`, id, CommandSent, CommandQueued)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// UpdateSystemCommandStatus records a result frame. Empty stage/message
// are stored as NULL.
func (d *DB) UpdateSystemCommandStatus(ctx context.Context, id, status, stage, message string) error {
	_, err := d.conn.ExecContext(ctx,
		`UPDATE system_commands
		    SET status = $2,
		        stage = COALESCE(NULLIF($3, ''), stage),
		        message = COALESCE(NULLIF($4, ''), message),
		        updated_at = NOW()
		  WHERE id = $1`, id, status, stage, message)
	return err
}

// ExpireSystemCommands marks commands that were never delivered within the
// window as expired, so a receiver that reconnects next week does not run a
// restart somebody asked for today.
func (d *DB) ExpireSystemCommands(ctx context.Context, olderThan time.Duration) (int64, error) {
	// Queued rows age from creation; sent/progress rows age from their last
	// update, so a receiver that stopped reporting mid-command does not
	// leave the row (and the UI polling it) waiting forever.
	res, err := d.conn.ExecContext(ctx,
		`UPDATE system_commands
		    SET status = $2,
		        message = COALESCE(message, CASE WHEN status = $3 THEN 'expired before delivery' ELSE 'expired without a result' END),
		        updated_at = NOW()
		  WHERE (status = $3 AND created_at < NOW() - $1::interval)
		     OR (status IN ('sent', 'progress') AND updated_at < NOW() - $1::interval)`,
		pgInterval(olderThan), CommandExpired, CommandQueued)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		if err := d.failCapturesOfExpiredCommands(ctx); err != nil {
			return n, err
		}
	}
	return n, nil
}

// ExpireUnansweredSystemCommands runs when a receiver connects: anything it
// was sent before and never reported on is expired, because a frame that
// was lost with a dropped socket will never be answered now.
func (d *DB) ExpireUnansweredSystemCommands(ctx context.Context, systemID int64, olderThan time.Duration) (int64, error) {
	res, err := d.conn.ExecContext(ctx,
		`UPDATE system_commands
		    SET status = $3,
		        message = COALESCE(message, 'receiver reconnected without reporting a result'),
		        updated_at = NOW()
		  WHERE system_id = $1 AND status IN ('sent', 'progress') AND updated_at < NOW() - $2::interval`,
		systemID, pgInterval(olderThan), CommandExpired)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		if err := d.failCapturesOfExpiredCommands(ctx); err != nil {
			return n, err
		}
	}
	return n, nil
}

// failCapturesOfExpiredCommands moves captures whose command expired out of
// the active states, so list views stop polling for them.
func (d *DB) failCapturesOfExpiredCommands(ctx context.Context) error {
	_, err := d.conn.ExecContext(ctx,
		`UPDATE system_captures AS c
		    SET status = 'error', error = COALESCE(c.error, 'command expired')
		   FROM system_commands AS m
		  WHERE c.command_id = m.id AND m.status = $1 AND c.status IN ('requested', 'recording')`,
		CommandExpired)
	return err
}

// --- captures ---

// InsertSystemCapture creates a capture row in status requested.
func (d *DB) InsertSystemCapture(ctx context.Context, capture *SystemCapture) error {
	return insertSystemCapture(ctx, d.conn, capture)
}

func insertSystemCapture(ctx context.Context, q execGetter, capture *SystemCapture) error {
	if capture.Status == "" {
		capture.Status = CaptureRequested
	}
	return q.GetContext(ctx, capture,
		`INSERT INTO system_captures
		   (id, system_id, command_id, kind, channel, seconds, sample_rate, status, requested_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING *`,
		capture.ID, capture.SystemID, capture.CommandID, capture.Kind, capture.Channel,
		capture.Seconds, capture.SampleRate, capture.Status, capture.RequestedBy)
}

// GetSystemCapture loads one capture.
func (d *DB) GetSystemCapture(ctx context.Context, id string) (*SystemCapture, error) {
	var c SystemCapture
	err := d.conn.GetContext(ctx, &c, `SELECT * FROM system_captures WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ListSystemCaptures returns the newest `limit` captures, newest first.
func (d *DB) ListSystemCaptures(ctx context.Context, systemID int64, limit int) ([]SystemCapture, error) {
	if limit < 1 || limit > 500 {
		limit = 50
	}
	out := []SystemCapture{}
	err := d.conn.SelectContext(ctx, &out,
		`SELECT * FROM system_captures WHERE system_id = $1
		  ORDER BY created_at DESC, id DESC LIMIT $2`, systemID, limit)
	return out, err
}

// UpdateSystemCaptureStatus sets status and (when non-empty) the error text.
func (d *DB) UpdateSystemCaptureStatus(ctx context.Context, id, status, errMsg string) error {
	_, err := d.conn.ExecContext(ctx,
		`UPDATE system_captures SET status = $2, error = NULLIF($3, '') WHERE id = $1`,
		id, status, errMsg)
	return err
}

// MarkSystemCaptureErrorByCommand fails the capture a command was recording,
// when the receiver reports the command itself failed. A capture that is
// already ready is left alone.
func (d *DB) MarkSystemCaptureErrorByCommand(ctx context.Context, commandID, errMsg string) error {
	_, err := d.conn.ExecContext(ctx,
		`UPDATE system_captures SET status = $2, error = NULLIF($3, '')
		  WHERE command_id = $1 AND status <> $4`,
		commandID, CaptureError, errMsg, CaptureReady)
	return err
}

// MarkSystemCaptureReady records a completed upload. sampleRate and channel
// come from the upload headers and only fill in what the request left
// unset.
func (d *DB) MarkSystemCaptureReady(ctx context.Context, id, path string, size int64, sampleRate int, channel string) error {
	_, err := d.conn.ExecContext(ctx,
		`UPDATE system_captures
		    SET status = $2, path = $3, size_bytes = $4, error = NULL, ready_at = NOW(),
		        sample_rate = COALESCE(sample_rate, NULLIF($5, 0)),
		        channel = COALESCE(channel, NULLIF($6, ''))
		  WHERE id = $1`,
		id, CaptureReady, path, size, sampleRate, channel)
	return err
}

// pgInterval renders a duration for a $n::interval parameter.
func pgInterval(d time.Duration) string {
	return fmt.Sprintf("%d seconds", int64(d.Seconds()))
}
