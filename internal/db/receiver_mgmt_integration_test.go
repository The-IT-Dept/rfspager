package db

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// managedSystemDB migrates a fresh schema (exercising 032) and seeds one
// approved receiver system. Needs RFSPAGER_TEST_DATABASE_URL like the other
// integration tests here.
func managedSystemDB(t *testing.T) (*DB, int64) {
	t.Helper()
	d := testMigrationDB(t)
	resetMigrationDB(t, d)
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return d, seedManagedSystem(t, d, "managed")
}

func seedManagedSystem(t *testing.T, d *DB, name string) int64 {
	t.Helper()
	ctx := context.Background()
	var userID int64
	if err := d.conn.QueryRowContext(ctx,
		`INSERT INTO users (user_id, email, password) VALUES (gen_random_uuid(), $1, '!') RETURNING id`,
		name+"@example.invalid").Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	var systemID int64
	if err := d.conn.QueryRowContext(ctx,
		`INSERT INTO systems (system_id, user_id, name, api_key, frequency, steal_capcodes, approved_at, created_at, updated_at)
		 VALUES (gen_random_uuid(), $1, $2, $3, 1, '0', NOW(), NOW(), NOW()) RETURNING id`,
		userID, name, "key-"+name).Scan(&systemID); err != nil {
		t.Fatalf("seed system: %v", err)
	}
	return systemID
}

func TestReceiverConfigRevisionsNumberSequentiallyUnderConcurrency(t *testing.T) {
	d, systemID := managedSystemDB(t)
	ctx := context.Background()

	if _, _, found, err := d.CurrentReceiverConfig(ctx, systemID); err != nil || found {
		t.Fatalf("fresh system: found=%v err=%v", found, err)
	}

	const writers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	versions := make(chan int, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			raw := []byte(fmt.Sprintf(`{"tuner_gain_tenths_db": %d}`, i*10))
			v, err := d.InsertReceiverConfigRevision(ctx, systemID, raw, "admin:test@example.invalid", "")
			if err != nil {
				t.Errorf("insert: %v", err)
				return
			}
			versions <- v
		}(i)
	}
	close(start)
	wg.Wait()
	close(versions)

	seen := map[int]bool{}
	for v := range versions {
		if seen[v] {
			t.Fatalf("version %d assigned twice", v)
		}
		seen[v] = true
	}
	for v := 1; v <= writers; v++ {
		if !seen[v] {
			t.Fatalf("version %d never assigned: %v", v, seen)
		}
	}

	version, raw, found, err := d.CurrentReceiverConfig(ctx, systemID)
	if err != nil || !found || version != writers || len(raw) == 0 {
		t.Fatalf("current = %d %q found=%v err=%v", version, raw, found, err)
	}
	revs, err := d.ListReceiverConfigRevisions(ctx, systemID, 3)
	if err != nil || len(revs) != 3 || revs[0].Version != writers || revs[2].Version != writers-2 {
		t.Fatalf("list = %+v err=%v", revs, err)
	}
	rev, err := d.GetReceiverConfigRevision(ctx, systemID, 2)
	if err != nil || rev.Version != 2 || rev.Source != "admin:test@example.invalid" || rev.Note != nil {
		t.Fatalf("get = %+v err=%v", rev, err)
	}
	if _, err := d.GetReceiverConfigRevision(ctx, systemID, 99); err != ErrNotFound {
		t.Fatalf("missing revision err = %v", err)
	}
}

func TestSeedReceiverConfigRevisionOnlyWritesOnce(t *testing.T) {
	d, systemID := managedSystemDB(t)
	ctx := context.Background()

	rev, seeded, err := d.SeedReceiverConfigRevision(ctx, systemID, []byte(`{"ppm": 7}`), "seeded-from-receiver", "seeded from receiver 5.5.0")
	if err != nil || !seeded || rev.Version != 1 || rev.Source != "seeded-from-receiver" || rev.Note == nil {
		t.Fatalf("first seed: rev=%+v seeded=%v err=%v", rev, seeded, err)
	}
	rev, seeded, err = d.SeedReceiverConfigRevision(ctx, systemID, []byte(`{"ppm": 9}`), "seeded-from-receiver", "")
	if err != nil || seeded || rev.Version != 1 || string(rev.Config) != `{"ppm": 7}` {
		t.Fatalf("second seed should return the existing revision: rev=%+v seeded=%v err=%v", rev, seeded, err)
	}
	if _, err := d.InsertReceiverConfigRevision(ctx, systemID, []byte(`{"ppm": 1}`), "admin:x", "note"); err != nil {
		t.Fatal(err)
	}
	rev, seeded, err = d.SeedReceiverConfigRevision(ctx, systemID, []byte(`{}`), "seeded-from-receiver", "")
	if err != nil || seeded || rev.Version != 2 || rev.Source != "admin:x" {
		t.Fatalf("seed after admin revision: rev=%+v seeded=%v err=%v", rev, seeded, err)
	}
}

func TestSystemConnectionManagementColumns(t *testing.T) {
	d, systemID := managedSystemDB(t)
	ctx := context.Background()
	if err := d.UpsertSystemConnection(ctx, &SystemConnection{SystemID: systemID, PodID: "pod:a"}); err != nil {
		t.Fatal(err)
	}
	conn, err := d.GetSystemConnection(ctx, systemID)
	if err != nil || conn.Proto != 1 || conn.ConfigVersion.Valid || conn.Host != nil {
		t.Fatalf("defaults: %+v err=%v", conn, err)
	}

	if err := d.UpdateSystemConnectionHello(ctx, systemID, "pod:a", 2, []byte(`{"model":"Raspberry Pi 4"}`), 0); err != nil {
		t.Fatal(err)
	}
	conn, _ = d.GetSystemConnection(ctx, systemID)
	if conn.Proto != 2 || conn.ConfigVersion.Valid || conn.ConfigStatus.Valid || string(conn.Host) != `{"model": "Raspberry Pi 4"}` {
		t.Fatalf("hello v0: %+v", conn)
	}
	if err := d.UpdateSystemConnectionHello(ctx, systemID, "pod:a", 2, nil, 3); err != nil {
		t.Fatal(err)
	}
	conn, _ = d.GetSystemConnection(ctx, systemID)
	if conn.ConfigVersion.Int64 != 3 || conn.ConfigStatus.String != "applied" || conn.Host != nil {
		t.Fatalf("hello v3: %+v", conn)
	}
	// Another pod's stale socket must not overwrite the live row.
	if err := d.UpdateSystemConnectionHello(ctx, systemID, "pod:stale", 1, nil, 0); err != nil {
		t.Fatal(err)
	}
	conn, _ = d.GetSystemConnection(ctx, systemID)
	if conn.Proto != 2 {
		t.Fatalf("stale pod overwrote proto: %+v", conn)
	}

	if err := d.UpdateSystemConnectionConfigStatus(ctx, systemID, 4, "rejected"); err != nil {
		t.Fatal(err)
	}
	conn, _ = d.GetSystemConnection(ctx, systemID)
	if conn.ConfigVersion.Int64 != 4 || conn.ConfigStatus.String != "rejected" {
		t.Fatalf("config status: %+v", conn)
	}

	at := time.Now().UTC().Truncate(time.Microsecond)
	stats := []byte(`{"interval_s":15,"channels":[{"label":"RFS","snr_db":12.5}]}`)
	if err := d.InsertSystemStats(ctx, systemID, at, stats); err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateSystemConnectionStats(ctx, systemID, stats, at); err != nil {
		t.Fatal(err)
	}
	conn, _ = d.GetSystemConnection(ctx, systemID)
	if !conn.LastStatsAt.Valid || !conn.LastStatsAt.Time.Equal(at) || len(conn.LastStats) == 0 {
		t.Fatalf("last stats: %+v", conn)
	}
	var decoded struct {
		IntervalS int `json:"interval_s"`
	}
	if err := json.Unmarshal(conn.LastStats, &decoded); err != nil || decoded.IntervalS != 15 {
		t.Fatalf("last_stats = %s (%v)", conn.LastStats, err)
	}
}

func TestQuerySystemStatsSamplesTheFirstReportPerBucket(t *testing.T) {
	d, systemID := managedSystemDB(t)
	ctx := context.Background()

	// A report every 15 s for the last 10 minutes, oldest first.
	now := time.Now().UTC().Truncate(time.Second)
	var inserted []time.Time
	for i := 40; i >= 1; i-- {
		at := now.Add(-time.Duration(i) * 15 * time.Second)
		if err := d.InsertSystemStats(ctx, systemID, at, []byte(fmt.Sprintf(`{"seq": %d}`, i))); err != nil {
			t.Fatal(err)
		}
		inserted = append(inserted, at)
	}

	rows, err := d.QuerySystemStats(ctx, systemID, time.Hour, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 10 || len(rows) > 11 {
		t.Fatalf("sampled %d rows for 10 minutes at 1 minute, want 10 or 11", len(rows))
	}
	for i, row := range rows {
		if i > 0 && !row.TS.After(rows[i-1].TS) {
			t.Fatalf("rows not ascending at %d: %v then %v", i, rows[i-1].TS, row.TS)
		}
		bucket := row.TS.Truncate(time.Minute)
		for _, at := range inserted {
			if at.Truncate(time.Minute).Equal(bucket) && at.Before(row.TS) {
				t.Fatalf("bucket %v returned %v but %v is earlier", bucket, row.TS, at)
			}
		}
		if len(row.Stats) == 0 {
			t.Fatalf("row %d has no stats", i)
		}
	}

	all, err := d.QuerySystemStats(ctx, systemID, time.Hour, time.Second)
	if err != nil || len(all) != 40 {
		t.Fatalf("1 s step should return every row: %d err=%v", len(all), err)
	}
	if none, err := d.QuerySystemStats(ctx, systemID, 2*time.Minute, time.Second); err != nil || len(none) > 8 {
		t.Fatalf("since window not applied: %d rows err=%v", len(none), err)
	}

	recent, err := d.RecentSystemStats(ctx, systemID, 5)
	if err != nil || len(recent) != 5 || !recent[4].TS.Equal(inserted[39]) || !recent[0].TS.Before(recent[4].TS) {
		t.Fatalf("recent = %+v err=%v", recent, err)
	}

	pruned, err := d.PruneSystemStats(ctx, 5*time.Minute)
	if err != nil || pruned < 19 || pruned > 21 {
		t.Fatalf("pruned %d err=%v, want about 20", pruned, err)
	}
}

func TestSystemCommandLifecycle(t *testing.T) {
	d, systemID := managedSystemDB(t)
	ctx := context.Background()

	cmd := &SystemCommand{ID: uuid.NewString(), SystemID: systemID, Type: "restart", RequestedBy: "admin:ops@example.invalid"}
	if err := d.InsertSystemCommand(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	if cmd.Status != CommandQueued || string(cmd.Params) != "{}" || cmd.CreatedAt.IsZero() {
		t.Fatalf("inserted = %+v", cmd)
	}

	queued, err := d.QueuedSystemCommands(ctx, systemID)
	if err != nil || len(queued) != 1 || queued[0].ID != cmd.ID {
		t.Fatalf("queued = %+v err=%v", queued, err)
	}
	if won, err := d.ClaimSystemCommand(ctx, cmd.ID); err != nil || !won {
		t.Fatalf("first claim: won=%v err=%v", won, err)
	}
	if won, err := d.ClaimSystemCommand(ctx, cmd.ID); err != nil || won {
		t.Fatalf("second claim must lose: won=%v err=%v", won, err)
	}
	if queued, _ := d.QueuedSystemCommands(ctx, systemID); len(queued) != 0 {
		t.Fatalf("claimed command still queued: %+v", queued)
	}

	if err := d.UpdateSystemCommandStatus(ctx, cmd.ID, CommandProgress, "stopping", ""); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetSystemCommand(ctx, cmd.ID)
	if err != nil || got.Status != CommandProgress || got.Stage == nil || *got.Stage != "stopping" || got.Message != nil {
		t.Fatalf("progress = %+v err=%v", got, err)
	}
	if err := d.UpdateSystemCommandStatus(ctx, cmd.ID, CommandOK, "", "restarting"); err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetSystemCommand(ctx, cmd.ID)
	// A result that omits stage keeps the last one, so the audit row never
	// loses detail mid-flight.
	if got.Status != CommandOK || got.Stage == nil || *got.Stage != "stopping" || *got.Message != "restarting" || !got.UpdatedAt.After(got.CreatedAt) {
		t.Fatalf("ok = %+v", got)
	}
	if _, err := d.GetSystemCommand(ctx, uuid.NewString()); err != ErrNotFound {
		t.Fatalf("missing command err = %v", err)
	}

	// Expiry only touches queued commands older than the window.
	stale := &SystemCommand{ID: uuid.NewString(), SystemID: systemID, Type: "dongle_reset", RequestedBy: "admin:x"}
	fresh := &SystemCommand{ID: uuid.NewString(), SystemID: systemID, Type: "dongle_reset", RequestedBy: "admin:x"}
	for _, c := range []*SystemCommand{stale, fresh} {
		if err := d.InsertSystemCommand(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.conn.ExecContext(ctx, `UPDATE system_commands SET created_at = NOW() - INTERVAL '2 hours' WHERE id = $1`, stale.ID); err != nil {
		t.Fatal(err)
	}
	n, err := d.ExpireSystemCommands(ctx, time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("expired %d err=%v", n, err)
	}
	got, _ = d.GetSystemCommand(ctx, stale.ID)
	if got.Status != CommandExpired || got.Message == nil {
		t.Fatalf("stale = %+v", got)
	}
	got, _ = d.GetSystemCommand(ctx, fresh.ID)
	if got.Status != CommandQueued {
		t.Fatalf("fresh = %+v", got)
	}
	if got, _ := d.GetSystemCommand(ctx, cmd.ID); got.Status != CommandOK {
		t.Fatalf("finished command was expired: %+v", got)
	}

	// A command that was sent but never answered expires from its last
	// update, and its capture row leaves the active states with it.
	ch, secs := "RFS", 5
	stuckCap := &SystemCapture{ID: uuid.NewString(), SystemID: systemID, Kind: "audio", Channel: &ch, Seconds: &secs, RequestedBy: "admin:x"}
	if err := d.InsertSystemCapture(ctx, stuckCap); err != nil {
		t.Fatal(err)
	}
	stuck := &SystemCommand{ID: uuid.NewString(), SystemID: systemID, Type: "capture", RequestedBy: "admin:x"}
	if err := d.InsertSystemCommand(ctx, stuck); err != nil {
		t.Fatal(err)
	}
	if _, err := d.conn.ExecContext(ctx, `UPDATE system_captures SET command_id = $2 WHERE id = $1`, stuckCap.ID, stuck.ID); err != nil {
		t.Fatal(err)
	}
	if won, _ := d.ClaimSystemCommand(ctx, stuck.ID); !won {
		t.Fatal("claim stuck")
	}
	// Recently sent: a reconnect must leave it alone...
	if n, err := d.ExpireUnansweredSystemCommands(ctx, systemID, 2*time.Minute); err != nil || n != 0 {
		t.Fatalf("fresh sent command expired: n=%d err=%v", n, err)
	}
	// ...but once it has gone unanswered past the grace, a reconnect expires it.
	if _, err := d.conn.ExecContext(ctx, `UPDATE system_commands SET updated_at = NOW() - INTERVAL '3 minutes' WHERE id = $1`, stuck.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := d.ExpireUnansweredSystemCommands(ctx, systemID, 2*time.Minute); err != nil || n != 1 {
		t.Fatalf("unanswered expiry: n=%d err=%v", n, err)
	}
	if got, _ := d.GetSystemCommand(ctx, stuck.ID); got.Status != CommandExpired {
		t.Fatalf("stuck = %+v", got)
	}
	if got, _ := d.GetSystemCapture(ctx, stuckCap.ID); got.Status != CaptureError || got.Error == nil {
		t.Fatalf("capture of expired command = %+v", got)
	}
	// The sweeper's expiry also ages sent/progress rows from their last update.
	prog := &SystemCommand{ID: uuid.NewString(), SystemID: systemID, Type: "update", RequestedBy: "admin:x"}
	if err := d.InsertSystemCommand(ctx, prog); err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateSystemCommandStatus(ctx, prog.ID, CommandProgress, "downloading", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := d.conn.ExecContext(ctx, `UPDATE system_commands SET updated_at = NOW() - INTERVAL '2 hours' WHERE id = $1`, prog.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := d.ExpireSystemCommands(ctx, time.Hour); err != nil || n != 1 {
		t.Fatalf("progress expiry: n=%d err=%v", n, err)
	}
	if got, _ := d.GetSystemCommand(ctx, prog.ID); got.Status != CommandExpired || got.Message == nil {
		t.Fatalf("stale progress = %+v", got)
	}

	list, err := d.ListSystemCommands(ctx, systemID, 10)
	if err != nil || len(list) != 5 {
		t.Fatalf("list = %d err=%v", len(list), err)
	}
}

func TestCaptureCommandAndCaptureRows(t *testing.T) {
	d, systemID := managedSystemDB(t)
	ctx := context.Background()

	channel, seconds := "RFS", 10
	capture := &SystemCapture{ID: uuid.NewString(), SystemID: systemID, Kind: "audio", Channel: &channel, Seconds: &seconds, RequestedBy: "admin:x"}
	cmd := &SystemCommand{ID: uuid.NewString(), SystemID: systemID, Type: "capture", RequestedBy: "admin:x",
		Params: []byte(fmt.Sprintf(`{"capture_id":%q,"kind":"audio","channel":"RFS","seconds":10}`, capture.ID))}
	if err := d.InsertSystemCaptureCommand(ctx, capture, cmd); err != nil {
		t.Fatal(err)
	}
	if capture.CommandID == nil || *capture.CommandID != cmd.ID || capture.Status != CaptureRequested || capture.Path != nil {
		t.Fatalf("capture = %+v", capture)
	}
	got, err := d.GetSystemCapture(ctx, capture.ID)
	if err != nil || *got.Channel != "RFS" || *got.Seconds != 10 || *got.CommandID != cmd.ID {
		t.Fatalf("get = %+v err=%v", got, err)
	}

	if err := d.MarkSystemCaptureErrorByCommand(ctx, cmd.ID, "dongle busy"); err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetSystemCapture(ctx, capture.ID)
	if got.Status != CaptureError || *got.Error != "dongle busy" {
		t.Fatalf("errored = %+v", got)
	}

	spectrum := &SystemCapture{ID: uuid.NewString(), SystemID: systemID, Kind: "spectrum", RequestedBy: "admin:x"}
	if err := d.InsertSystemCapture(ctx, spectrum); err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateSystemCaptureStatus(ctx, spectrum.ID, CaptureRecording, ""); err != nil {
		t.Fatal(err)
	}
	if err := d.MarkSystemCaptureReady(ctx, spectrum.ID, "/var/lib/captures/1/x.json", 4096, 1_920_000, ""); err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetSystemCapture(ctx, spectrum.ID)
	if got.Status != CaptureReady || *got.Path != "/var/lib/captures/1/x.json" || *got.SizeBytes != 4096 ||
		*got.SampleRate != 1_920_000 || got.Channel != nil || got.ReadyAt == nil || got.Error != nil {
		t.Fatalf("ready = %+v", got)
	}
	// A ready capture is not failed by a late command error.
	if err := d.MarkSystemCaptureErrorByCommand(ctx, cmd.ID, "late"); err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetSystemCapture(ctx, spectrum.ID)
	if got.Status != CaptureReady {
		t.Fatalf("ready capture was failed: %+v", got)
	}

	list, err := d.ListSystemCaptures(ctx, systemID, 10)
	if err != nil || len(list) != 2 || list[0].ID != spectrum.ID {
		t.Fatalf("list = %+v err=%v", list, err)
	}

	// Deleting the command keeps the capture (command_id → NULL).
	if _, err := d.conn.ExecContext(ctx, `DELETE FROM system_commands WHERE id = $1`, cmd.ID); err != nil {
		t.Fatal(err)
	}
	got, err = d.GetSystemCapture(ctx, capture.ID)
	if err != nil || got.CommandID != nil {
		t.Fatalf("capture after command delete = %+v err=%v", got, err)
	}
}

func TestReceiverManagementTriggersNotify(t *testing.T) {
	d, systemID := managedSystemDB(t)
	ctx := context.Background()

	listener, err := NewListener(os.Getenv("RFSPAGER_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close(context.Background()) })
	commandCh, err := listener.Listen("receiver_command", 4)
	if err != nil {
		t.Fatal(err)
	}
	configCh, err := listener.Listen("receiver_config", 4)
	if err != nil {
		t.Fatal(err)
	}

	cmd := &SystemCommand{ID: uuid.NewString(), SystemID: systemID, Type: "restart", RequestedBy: "admin:x"}
	if err := d.InsertSystemCommand(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	select {
	case payload := <-commandCh:
		if payload != cmd.ID {
			t.Fatalf("receiver_command payload = %q, want command id %q", payload, cmd.ID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no receiver_command notification after inserting a command")
	}

	if _, err := d.InsertReceiverConfigRevision(ctx, systemID, []byte(`{"ppm": 1}`), "admin:x", ""); err != nil {
		t.Fatal(err)
	}
	select {
	case payload := <-configCh:
		if payload != fmt.Sprint(systemID) {
			t.Fatalf("receiver_config payload = %q, want system id %d", payload, systemID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no receiver_config notification after inserting a revision")
	}
}
