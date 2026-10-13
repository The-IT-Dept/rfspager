package control

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/the-it-dept/rfspager/internal/receiver/sdr"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// fakeDevice records the tuning calls a live apply makes.
type fakeDevice struct {
	mu    sync.Mutex
	gains []int
	ppms  []int
	fail  bool
}

func (d *fakeDevice) Index() int                 { return 0 }
func (d *fakeDevice) Name() string               { return "fake" }
func (d *fakeDevice) Tune(uint32) error          { return nil }
func (d *fakeDevice) SetSampleRate(uint32) error { return nil }
func (d *fakeDevice) Close() error               { return nil }
func (d *fakeDevice) ReadIQ(context.Context, int) (<-chan sdr.Buffer, error) {
	return nil, errors.New("not implemented")
}
func (d *fakeDevice) SetGain(g int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.fail {
		return errors.New("tuner refused")
	}
	d.gains = append(d.gains, g)
	return nil
}
func (d *fakeDevice) SetPPM(p int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ppms = append(d.ppms, p)
	return nil
}
func (d *fakeDevice) calls() ([]int, []int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]int{}, d.gains...), append([]int{}, d.ppms...)
}

// fakeSender records results.
type fakeSender struct {
	mu      sync.Mutex
	configs []rxproto.ConfigResult
	cmds    []rxproto.CommandResult
	drained int
}

func (s *fakeSender) SendConfigResult(r rxproto.ConfigResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configs = append(s.configs, r)
}
func (s *fakeSender) SendCommandResult(r rxproto.CommandResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cmds = append(s.cmds, r)
}
func (s *fakeSender) Drain(context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.drained++
}
func (s *fakeSender) commandResults() []rxproto.CommandResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]rxproto.CommandResult{}, s.cmds...)
}

func restartRequested(m *Manager) bool {
	select {
	case <-m.RestartRequests():
		return true
	case <-time.After(50 * time.Millisecond):
		return false
	}
}

func TestApplyLiveChangesTouchDeviceOnly(t *testing.T) {
	store := NewStore(t.TempDir())
	level := &slog.LevelVar{}
	var postAll []bool
	sender := &fakeSender{}
	m := NewManager(Options{
		Store: store, Version: 0, Config: rxproto.Defaults(),
		Level: level, SetPostAll: func(v bool) { postAll = append(postAll, v) }, Sender: sender,
	})
	dev := &fakeDevice{}
	m.RegisterDevice(dev, rxproto.Defaults())

	cfg := rxproto.Defaults()
	cfg.TunerGainTenthsDB = 496
	cfg.PPM = -2
	cfg.LogLevel = "debug"
	cfg.StatsIntervalS = 30
	cfg.PostAll = true
	res := m.Apply(rxproto.ConfigPayload{Version: 3, Config: cfg})
	if res.Status != rxproto.ConfigApplied || res.Version != 3 || res.Effective.TunerGainTenthsDB != 496 {
		t.Fatalf("result: %+v", res)
	}
	gains, ppms := dev.calls()
	if len(gains) != 1 || gains[0] != 496 || len(ppms) != 1 || ppms[0] != -2 {
		t.Fatalf("device calls gains=%v ppms=%v", gains, ppms)
	}
	if restartRequested(m) {
		t.Fatal("a live change must not restart the dongle")
	}
	if level.Level() != slog.LevelDebug {
		t.Errorf("log level %v", level.Level())
	}
	if len(postAll) != 1 || !postAll[0] {
		t.Errorf("post_all callback: %v", postAll)
	}
	if m.Version() != 3 || m.Config().StatsIntervalS != 30 {
		t.Errorf("manager state: v=%d cfg=%+v", m.Version(), m.Config())
	}
	if v, c, ok := store.LoadConfig(); !ok || v != 3 || c.TunerGainTenthsDB != 496 {
		t.Errorf("not persisted: v=%d ok=%v", v, ok)
	}
	if len(sender.configs) != 1 || sender.configs[0].Status != rxproto.ConfigApplied {
		t.Errorf("config_result not sent: %+v", sender.configs)
	}
	if hc := m.HelloConfig(); hc.Version != 3 || hc.Effective.PPM != -2 {
		t.Errorf("hello config: %+v", hc)
	}

	// Same values again: nothing touches the device.
	m.Apply(rxproto.ConfigPayload{Version: 4, Config: cfg})
	if g, p := dev.calls(); len(g) != 1 || len(p) != 1 {
		t.Errorf("unchanged values re-sent to the device: gains=%v ppms=%v", g, p)
	}
}

func TestApplyNonLiveChangeRestartsDongle(t *testing.T) {
	store := NewStore(t.TempDir())
	m := NewManager(Options{Store: store, Config: rxproto.Defaults()})
	dev := &fakeDevice{}
	m.RegisterDevice(dev, rxproto.Defaults())

	for name, mut := range map[string]func(*rxproto.RuntimeConfig){
		"sample rate":   func(c *rxproto.RuntimeConfig) { c.SampleRate = 1_536_000 },
		"dongle serial": func(c *rxproto.RuntimeConfig) { c.DongleSerial = "RFS00005" },
		"dongle index":  func(c *rxproto.RuntimeConfig) { c.DongleIndex = 1 },
		"channel":       func(c *rxproto.RuntimeConfig) { c.Channels[1].Enabled = false },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := rxproto.Defaults()
			mut(&cfg)
			res := m.Apply(rxproto.ConfigPayload{Version: 2, Config: cfg})
			if res.Status != rxproto.ConfigApplied {
				t.Fatalf("result: %+v", res)
			}
			if !restartRequested(m) {
				t.Fatal("expected a dongle restart request")
			}
			if v, c, ok := store.LoadConfig(); !ok || v != 2 || !equalConfig(c, cfg) {
				t.Errorf("not persisted: v=%d ok=%v", v, ok)
			}
			// Back to defaults for the next case (also a non-live change).
			m.Apply(rxproto.ConfigPayload{Version: 2, Config: rxproto.Defaults()})
			restartRequested(m)
		})
	}
	if g, p := dev.calls(); len(g) != 0 || len(p) != 0 {
		t.Errorf("non-live changes must not touch the device: gains=%v ppms=%v", g, p)
	}
}

func TestApplyWithoutDeviceDoesNotRequestRestart(t *testing.T) {
	m := NewManager(Options{Config: rxproto.Defaults()})
	cfg := rxproto.Defaults()
	cfg.SampleRate = 1_536_000
	m.Apply(rxproto.ConfigPayload{Version: 2, Config: cfg})
	if restartRequested(m) {
		t.Fatal("no device open: the next open picks the config up, no restart needed")
	}
	// The dongle loop opened with the stale config: registering catches it.
	m.RegisterDevice(&fakeDevice{}, rxproto.Defaults())
	if !restartRequested(m) {
		t.Fatal("registering a device opened with a stale config must request a restart")
	}
	// Opened with a config that only differs in gain: applied live.
	stale := cfg
	stale.TunerGainTenthsDB = 100
	dev := &fakeDevice{}
	m.RegisterDevice(dev, stale)
	if g, _ := dev.calls(); len(g) != 1 || g[0] != 0 {
		t.Fatalf("gain gap not applied live: %v", g)
	}
	if restartRequested(m) {
		t.Fatal("gain gap should not restart")
	}
}

func TestApplyRejectsInvalidConfig(t *testing.T) {
	store := NewStore(t.TempDir())
	sender := &fakeSender{}
	m := NewManager(Options{Store: store, Version: 1, Config: rxproto.Defaults(), Sender: sender})
	dev := &fakeDevice{}
	m.RegisterDevice(dev, rxproto.Defaults())
	cfg := rxproto.Defaults()
	cfg.TunerGainTenthsDB = 999
	cfg.Channels[0].Label = ""
	res := m.Apply(rxproto.ConfigPayload{Version: 2, Config: cfg})
	if res.Status != rxproto.ConfigRejected || res.Errors["tuner_gain_tenths_db"] == "" || res.Errors["channels[0].label"] == "" {
		t.Fatalf("result: %+v", res)
	}
	if res.Effective.TunerGainTenthsDB != 0 || m.Version() != 1 {
		t.Errorf("rejected config must leave the running one alone: %+v v=%d", res.Effective, m.Version())
	}
	if _, _, ok := store.LoadConfig(); ok {
		t.Error("rejected config must not be persisted")
	}
	if g, _ := dev.calls(); len(g) != 0 {
		t.Error("rejected config touched the device")
	}
	if restartRequested(m) {
		t.Error("rejected config requested a restart")
	}
	if len(sender.configs) != 1 || sender.configs[0].Status != rxproto.ConfigRejected {
		t.Errorf("config_result: %+v", sender.configs)
	}
}

func TestLiveApplyFailureFallsBackToRestart(t *testing.T) {
	m := NewManager(Options{Config: rxproto.Defaults()})
	dev := &fakeDevice{fail: true}
	m.RegisterDevice(dev, rxproto.Defaults())
	cfg := rxproto.Defaults()
	cfg.TunerGainTenthsDB = 200
	if res := m.Apply(rxproto.ConfigPayload{Version: 2, Config: cfg}); res.Status != rxproto.ConfigApplied {
		t.Fatalf("result: %+v", res)
	}
	if !restartRequested(m) {
		t.Fatal("a device that refuses a live change must be reopened")
	}
}

func TestHandleRestartAndDongleReset(t *testing.T) {
	sender := &fakeSender{}
	var exits []int
	m := NewManager(Options{Config: rxproto.Defaults(), Sender: sender, Exit: func(c int) { exits = append(exits, c) }, ExitGrace: 10 * time.Millisecond})
	m.Handle(rxproto.Command{ID: "r1", Type: rxproto.CmdRestart})
	if len(exits) != 1 || exits[0] != 0 {
		t.Fatalf("exit codes %v", exits)
	}
	if sender.drained != 1 {
		t.Errorf("sender not drained before exit")
	}
	res := sender.commandResults()
	if len(res) != 1 || res[0].ID != "r1" || res[0].Status != rxproto.CmdOK {
		t.Fatalf("restart result: %+v", res)
	}

	m.Handle(rxproto.Command{ID: "d1", Type: rxproto.CmdDongleReset})
	if !restartRequested(m) {
		t.Fatal("dongle_reset must request a restart")
	}
	res = sender.commandResults()
	if len(res) != 2 || res[1].ID != "d1" || res[1].Status != rxproto.CmdOK {
		t.Fatalf("dongle_reset result: %+v", res)
	}

	m.Handle(rxproto.Command{ID: "x1", Type: "shell"})
	if res = sender.commandResults(); len(res) != 3 || res[2].Status != rxproto.CmdError {
		t.Fatalf("unknown command: %+v", res)
	}

	m.Handle(rxproto.Command{ID: "u1", Type: rxproto.CmdUpdate, Params: json.RawMessage(`{"version":"v1.0.0"}`)})
	if res = sender.commandResults(); len(res) != 4 || res[3].Status != rxproto.CmdError {
		t.Fatalf("update without an updater: %+v", res)
	}
}

func TestConfirmUpdateReportsAndClearsMarker(t *testing.T) {
	store := NewStore(t.TempDir())
	sender := &fakeSender{}
	m := NewManager(Options{Store: store, Config: rxproto.Defaults(), Sender: sender})
	marker := &UpdateMarker{CommandID: "u9", From: "5.4.0-abc", To: "v5.5.0", StartedAt: time.Now(), Boots: 1}
	if err := store.SaveMarker(marker); err != nil {
		t.Fatal(err)
	}
	m.ConfirmUpdate(marker, "5.5.0-def1234")
	res := sender.commandResults()
	if len(res) != 1 || res[0].ID != "u9" || res[0].Type != rxproto.CmdUpdate || res[0].Status != rxproto.CmdOK || res[0].Stage != "running" {
		t.Fatalf("confirm result: %+v", res)
	}
	if mk, _ := store.LoadMarker(); mk != nil {
		t.Fatal("marker not removed")
	}
	// Nil marker is a no-op.
	m.ConfirmUpdate(nil, "x")
	if len(sender.commandResults()) != 1 {
		t.Fatal("nil marker sent a result")
	}
}

func equalConfig(a, b rxproto.RuntimeConfig) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}
