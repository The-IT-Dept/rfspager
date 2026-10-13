package recvhub

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/the-it-dept/rfspager/internal/db"
	"github.com/the-it-dept/rfspager/internal/models"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// testHub connects to RFSPAGER_TEST_DATABASE_URL, migrates a fresh schema and
// seeds one approved receiver system. The hub is built without a listener;
// these tests drive it directly rather than through NOTIFY.
func testHub(t *testing.T) (*Hub, *db.DB, *models.System) {
	t.Helper()
	rawURL := os.Getenv("RFSPAGER_TEST_DATABASE_URL")
	if rawURL == "" {
		t.Skip("RFSPAGER_TEST_DATABASE_URL is not set")
	}
	u, err := url.Parse(rawURL)
	if err != nil || !strings.HasPrefix(strings.TrimPrefix(u.Path, "/"), "rfspager_test") {
		t.Fatalf("refusing to reset non-test database %q", rawURL)
	}
	database, err := db.New(rawURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	if _, err := database.Conn().ExecContext(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	sys := seedHubSystem(t, database, "budgewoi")
	PodID = "test-pod"
	hub, err := New(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	return hub, database, sys
}

func seedHubSystem(t *testing.T, database *db.DB, name string) *models.System {
	t.Helper()
	ctx := context.Background()
	var userID int64
	if err := database.Conn().QueryRowContext(ctx,
		`INSERT INTO users (user_id, email, password) VALUES (gen_random_uuid(), $1, '!') RETURNING id`,
		name+"@example.invalid").Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := database.Conn().ExecContext(ctx,
		`INSERT INTO systems (system_id, user_id, name, api_key, frequency, steal_capcodes, approved_at, created_at, updated_at)
		 VALUES (gen_random_uuid(), $1, $2, $3, 1, '0', NOW(), NOW(), NOW())`,
		userID, name, "key-"+name); err != nil {
		t.Fatalf("seed system: %v", err)
	}
	sys, err := database.GetSystemByAPIKey(ctx, "key-"+name)
	if err != nil {
		t.Fatalf("load seeded system: %v", err)
	}
	return sys
}

// countEvents returns how many events of a type a system has, and the
// message of the newest one.
func countEvents(t *testing.T, database *db.DB, systemID int64, evtType string) (int, string) {
	t.Helper()
	var n int
	var message string
	if err := database.Conn().QueryRowContext(context.Background(),
		`SELECT COUNT(*),
		        COALESCE((SELECT message FROM system_events
		                   WHERE system_id = $1 AND type = $2
		                   ORDER BY created_at DESC, id DESC LIMIT 1), '')
		   FROM system_events WHERE system_id = $1 AND type = $2`,
		systemID, evtType).Scan(&n, &message); err != nil {
		t.Fatal(err)
	}
	return n, message
}

func protoTwoConn(t *testing.T, hub *Hub, database *db.DB, sys *models.System) *Conn {
	t.Helper()
	c := newConn(hub, nil, sys)
	c.proto = 2
	ctx := context.Background()
	if err := database.UpsertSystemConnection(ctx, &db.SystemConnection{SystemID: sys.ID, PodID: c.podID}); err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateSystemConnectionHello(ctx, sys.ID, c.podID, 2, []byte(`{"model":"Pi 4"}`), 0); err != nil {
		t.Fatal(err)
	}
	hub.Register(c)
	return c
}

func takeFrame(t *testing.T, c *Conn) Frame {
	t.Helper()
	select {
	case f := <-c.sendCh:
		return f
	case <-time.After(time.Second):
		t.Fatal("no frame queued for the receiver")
		return Frame{}
	}
}

func TestWelcomeConfigSeedsRevisionFromReceiverHello(t *testing.T) {
	hub, database, sys := testHub(t)
	ctx := context.Background()

	effective := rxproto.Defaults()
	effective.TunerGainTenthsDB = 280
	hello := &HelloPayload{Version: "5.5.0", Proto: 2, Config: &rxproto.HelloConfig{Version: 0, Effective: effective}}

	payload := hub.welcomeConfig(ctx, sys.ID, hello)
	if payload == nil || payload.Version != 1 || payload.Config.TunerGainTenthsDB != 280 {
		t.Fatalf("welcome config = %+v", payload)
	}
	revs, err := database.ListReceiverConfigRevisions(ctx, sys.ID, 10)
	if err != nil || len(revs) != 1 || revs[0].Source != "seeded-from-receiver" || revs[0].Note == nil || !strings.Contains(*revs[0].Note, "5.5.0") {
		t.Fatalf("revisions = %+v err=%v", revs, err)
	}

	// Reconnecting with a different reported config does not reseed.
	effective.TunerGainTenthsDB = 100
	if again := hub.welcomeConfig(ctx, sys.ID, hello); again == nil || again.Version != 1 || again.Config.TunerGainTenthsDB != 280 {
		t.Fatalf("second welcome reseeded: %+v", again)
	}

	// An admin revision supersedes, normalised over the defaults.
	if _, err := database.InsertReceiverConfigRevision(ctx, sys.ID, []byte(`{"tuner_gain_tenths_db": 400}`), "admin:x", ""); err != nil {
		t.Fatal(err)
	}
	if next := hub.welcomeConfig(ctx, sys.ID, hello); next == nil || next.Version != 2 || next.Config.TunerGainTenthsDB != 400 || next.Config.SampleRate != 1_920_000 {
		t.Fatalf("welcome after admin revision: %+v", next)
	}
}

func TestWelcomeConfigSeedsDefaultsWhenReceiverReportsInvalidConfig(t *testing.T) {
	hub, database, sys := testHub(t)
	ctx := context.Background()

	bad := rxproto.Defaults()
	bad.SampleRate = 1
	payload := hub.welcomeConfig(ctx, sys.ID, &HelloPayload{Version: "5.5.0", Proto: 2, Config: &rxproto.HelloConfig{Effective: bad}})
	if payload == nil || payload.Version != 1 || payload.Config.SampleRate != 1_920_000 {
		t.Fatalf("welcome config = %+v", payload)
	}
	revs, _ := database.ListReceiverConfigRevisions(ctx, sys.ID, 1)
	if len(revs) != 1 || revs[0].Note == nil || !strings.Contains(*revs[0].Note, "invalid") {
		t.Fatalf("seeded revision = %+v", revs)
	}

	// No config at all in hello (should not happen for proto 2, but a
	// receiver mid-upgrade might) also seeds the defaults.
	other := seedHubSystem(t, database, "figtree")
	if p := hub.welcomeConfig(ctx, other.ID, &HelloPayload{Proto: 2}); p == nil || p.Version != 1 || p.Config.TunerGainTenthsDB != 0 {
		t.Fatalf("welcome without config = %+v", p)
	}
}

func TestHandleFrameRecordsStatsConfigAndCommandResults(t *testing.T) {
	hub, database, sys := testHub(t)
	ctx := context.Background()
	c := protoTwoConn(t, hub, database, sys)

	// stats: persisted, cached on the connection row, fanned out.
	sub, cancel := hub.Stats.Subscribe(sys.ID)
	defer cancel()
	stats := rxproto.Stats{IntervalS: 15, Dongle: rxproto.DongleStats{Open: true, GainTenthsDB: 280},
		Channels: []rxproto.ChannelStats{{Label: "RFS", SNRDB: 12.5}}}
	c.handleFrame(ctx, database, Frame{Type: rxproto.FrameStats, Data: mustJSON(stats)})
	select {
	case raw := <-sub:
		var env struct {
			TS    int64         `json:"ts"`
			Stats rxproto.Stats `json:"stats"`
		}
		if err := json.Unmarshal(raw, &env); err != nil || env.TS == 0 || env.Stats.Dongle.GainTenthsDB != 280 {
			t.Fatalf("stats envelope %s (%v)", raw, err)
		}
	case <-time.After(time.Second):
		t.Fatal("stats were not fanned out")
	}
	if rows, err := database.RecentSystemStats(ctx, sys.ID, 10); err != nil || len(rows) != 1 {
		t.Fatalf("stored stats = %d err=%v", len(rows), err)
	}
	conn, _ := database.GetSystemConnection(ctx, sys.ID)
	if !conn.LastStatsAt.Valid || !strings.Contains(string(conn.LastStats), `"RFS"`) {
		t.Fatalf("connection stats cache = %+v", conn)
	}

	// config_result: applied then rejected, each with its event.
	c.handleFrame(ctx, database, Frame{Type: rxproto.FrameConfigResult, Data: mustJSON(rxproto.ConfigResult{Version: 3, Status: rxproto.ConfigApplied})})
	conn, _ = database.GetSystemConnection(ctx, sys.ID)
	if conn.ConfigVersion.Int64 != 3 || conn.ConfigStatus.String != "applied" {
		t.Fatalf("applied: %+v", conn)
	}
	if n, _ := countEvents(t, database, sys.ID, EvtReceiverConfigApplied); n != 1 {
		t.Fatalf("applied events = %d", n)
	}
	c.handleFrame(ctx, database, Frame{Type: rxproto.FrameConfigResult, Data: mustJSON(rxproto.ConfigResult{
		Version: 4, Status: rxproto.ConfigRejected, Errors: map[string]string{"ppm": "must be between -200 and 200"}})})
	conn, _ = database.GetSystemConnection(ctx, sys.ID)
	if conn.ConfigVersion.Int64 != 4 || conn.ConfigStatus.String != "rejected" {
		t.Fatalf("rejected: %+v", conn)
	}
	if n, msg := countEvents(t, database, sys.ID, EvtReceiverConfigRejected); n != 1 || !strings.Contains(msg, "ppm") {
		t.Fatalf("rejected events = %d %q", n, msg)
	}
	// Junk statuses are ignored.
	c.handleFrame(ctx, database, Frame{Type: rxproto.FrameConfigResult, Data: mustJSON(rxproto.ConfigResult{Version: 5, Status: "maybe"})})
	if conn, _ = database.GetSystemConnection(ctx, sys.ID); conn.ConfigVersion.Int64 != 4 {
		t.Fatalf("unknown status changed the row: %+v", conn)
	}

	// Command delivery: claimed exactly once, frame carries the command.
	cmd := &db.SystemCommand{ID: uuid.NewString(), SystemID: sys.ID, Type: rxproto.CmdRestart, RequestedBy: "admin:x"}
	if err := database.InsertSystemCommand(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	if !hub.DeliverCommand(ctx, cmd.ID) {
		t.Fatal("command was not delivered to the live proto-2 conn")
	}
	frame := takeFrame(t, c)
	var sent rxproto.Command
	if frame.Type != rxproto.FrameCommand || frame.ID != cmd.ID || json.Unmarshal(frame.Data, &sent) != nil || sent.ID != cmd.ID || sent.Type != rxproto.CmdRestart {
		t.Fatalf("command frame = %+v", frame)
	}
	if got, _ := database.GetSystemCommand(ctx, cmd.ID); got.Status != db.CommandSent {
		t.Fatalf("after delivery status = %s", got.Status)
	}
	if hub.DeliverCommand(ctx, cmd.ID) || len(c.sendCh) != 0 {
		t.Fatal("a sent command was delivered twice")
	}

	// command_result: progress updates the row quietly, ok writes an event.
	c.handleFrame(ctx, database, Frame{Type: rxproto.FrameCommandResult, Data: mustJSON(rxproto.CommandResult{ID: cmd.ID, Type: cmd.Type, Status: rxproto.CmdProgress, Stage: "stopping"})})
	got, _ := database.GetSystemCommand(ctx, cmd.ID)
	if got.Status != db.CommandProgress || got.Stage == nil || *got.Stage != "stopping" {
		t.Fatalf("progress = %+v", got)
	}
	if n, _ := countEvents(t, database, sys.ID, EvtReceiverCommand); n != 0 {
		t.Fatalf("progress wrote %d events", n)
	}
	c.handleFrame(ctx, database, Frame{Type: rxproto.FrameCommandResult, Data: mustJSON(rxproto.CommandResult{ID: cmd.ID, Type: cmd.Type, Status: rxproto.CmdOK, Message: "exiting for systemd"})})
	got, _ = database.GetSystemCommand(ctx, cmd.ID)
	if got.Status != db.CommandOK || got.Stage == nil || *got.Stage != "stopping" || *got.Message != "exiting for systemd" {
		t.Fatalf("ok = %+v", got)
	}
	if n, msg := countEvents(t, database, sys.ID, EvtReceiverCommand); n != 1 || !strings.Contains(msg, "restart succeeded") {
		t.Fatalf("command events = %d %q", n, msg)
	}

	// A result for another system's command is ignored.
	other := seedHubSystem(t, database, "figtree")
	foreign := &db.SystemCommand{ID: uuid.NewString(), SystemID: other.ID, Type: rxproto.CmdRestart, RequestedBy: "admin:x"}
	if err := database.InsertSystemCommand(ctx, foreign); err != nil {
		t.Fatal(err)
	}
	c.handleFrame(ctx, database, Frame{Type: rxproto.FrameCommandResult, Data: mustJSON(rxproto.CommandResult{ID: foreign.ID, Status: rxproto.CmdOK})})
	if got, _ := database.GetSystemCommand(ctx, foreign.ID); got.Status != db.CommandQueued {
		t.Fatalf("foreign command was updated: %+v", got)
	}

	// A failed capture command fails its capture row.
	channel, seconds := "RFS", 5
	capture := &db.SystemCapture{ID: uuid.NewString(), SystemID: sys.ID, Kind: rxproto.CaptureAudio, Channel: &channel, Seconds: &seconds, RequestedBy: "admin:x"}
	capCmd := &db.SystemCommand{ID: uuid.NewString(), SystemID: sys.ID, Type: rxproto.CmdCapture, RequestedBy: "admin:x",
		Params: mustJSON(rxproto.CaptureParams{CaptureID: capture.ID, Kind: rxproto.CaptureAudio, Channel: "RFS", Seconds: 5})}
	if err := database.InsertSystemCaptureCommand(ctx, capture, capCmd); err != nil {
		t.Fatal(err)
	}
	if !hub.DeliverCommand(ctx, capCmd.ID) {
		t.Fatal("capture command not delivered")
	}
	takeFrame(t, c)
	c.handleFrame(ctx, database, Frame{Type: rxproto.FrameCommandResult, Data: mustJSON(rxproto.CommandResult{ID: capCmd.ID, Type: rxproto.CmdCapture, Status: rxproto.CmdError, Stage: "record", Message: "dongle busy"})})
	gotCap, _ := database.GetSystemCapture(ctx, capture.ID)
	if gotCap.Status != db.CaptureError || gotCap.Error == nil || *gotCap.Error != "dongle busy" {
		t.Fatalf("capture after failed command = %+v", gotCap)
	}
	if n, msg := countEvents(t, database, sys.ID, EvtReceiverCommand); n != 2 || !strings.Contains(msg, "dongle busy") {
		t.Fatalf("command events after failure = %d %q", n, msg)
	}
}

func TestCommandsWaitForAProtoTwoConnection(t *testing.T) {
	hub, database, sys := testHub(t)
	ctx := context.Background()

	old := newConn(hub, nil, sys) // proto 1: a 5.3.x receiver
	hub.Register(old)
	cmd := &db.SystemCommand{ID: uuid.NewString(), SystemID: sys.ID, Type: rxproto.CmdDongleReset, RequestedBy: "admin:x"}
	if err := database.InsertSystemCommand(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	if hub.DeliverCommand(ctx, cmd.ID) || len(old.sendCh) != 0 {
		t.Fatal("command was sent to a proto-1 receiver")
	}
	if got, _ := database.GetSystemCommand(ctx, cmd.ID); got.Status != db.CommandQueued {
		t.Fatalf("status = %s, want queued", got.Status)
	}
	hub.pushConfig(ctx, sys.ID)
	if len(old.sendCh) != 0 {
		t.Fatal("config was pushed to a proto-1 receiver")
	}

	// The updated receiver reconnects: queued commands go out behind the
	// welcome, and config pushes reach it.
	c := protoTwoConn(t, hub, database, sys)
	select {
	case <-old.closed:
	default:
		t.Fatal("registering the new conn did not close the old one")
	}
	hub.deliverQueued(ctx, c)
	if frame := takeFrame(t, c); frame.Type != rxproto.FrameCommand || frame.ID != cmd.ID {
		t.Fatalf("queued command frame = %+v", frame)
	}
	if got, _ := database.GetSystemCommand(ctx, cmd.ID); got.Status != db.CommandSent {
		t.Fatalf("status = %s, want sent", got.Status)
	}

	hub.pushConfig(ctx, sys.ID) // no revision yet: nothing to push
	if len(c.sendCh) != 0 {
		t.Fatal("config pushed for a system with no revision")
	}
	if _, err := database.InsertReceiverConfigRevision(ctx, sys.ID, []byte(`{"ppm": 12}`), "admin:x", ""); err != nil {
		t.Fatal(err)
	}
	hub.pushConfig(ctx, sys.ID)
	frame := takeFrame(t, c)
	var payload rxproto.ConfigPayload
	if frame.Type != rxproto.FrameConfig || json.Unmarshal(frame.Data, &payload) != nil || payload.Version != 1 || payload.Config.PPM != 12 || payload.Config.SampleRate != 1_920_000 {
		t.Fatalf("config frame = %+v", frame)
	}
}
