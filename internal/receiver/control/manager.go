package control

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/the-it-dept/rfspager/internal/receiver"
	"github.com/the-it-dept/rfspager/internal/receiver/sdr"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// Sender is the primary websocket client as the manager sees it.
type Sender interface {
	SendConfigResult(rxproto.ConfigResult)
	SendCommandResult(rxproto.CommandResult)
	// Drain waits for queued frames to be written, or ctx to expire.
	Drain(ctx context.Context)
}

// Taps is what captures need from the running pipeline. Implemented by the
// dongle loop owner in cmd/receiver.
type Taps interface {
	// Channel returns the live channel with this label, or nil.
	Channel(label string) *receiver.Channel
	// Spectrum attaches an accumulator to the raw IQ stream. It fails when
	// the dongle is closed or another spectrum is in progress.
	Spectrum(s *receiver.Spectrum) error
	// CancelSpectrum detaches an accumulator that will not be waited for.
	CancelSpectrum(s *receiver.Spectrum)
}

// Options wires a Manager into the receiver. Everything but Version and
// Config is optional, so tests can build a Manager with just the parts
// they exercise.
type Options struct {
	Store   *Store
	Version int                   // config version at startup (0 = bootstrap)
	Config  rxproto.RuntimeConfig // config at startup

	Level      *slog.LevelVar // log level, applied live
	SetPostAll func(bool)     // the Forwarder's content-filter bypass
	Sender     Sender
	Taps       Taps
	Updater    *Updater // nil disables the update command

	// ServerURL and APIKey are the primary endpoint's, used for capture
	// uploads.
	ServerURL string
	APIKey    string
	HTTP      *http.Client

	Exit      func(code int) // default os.Exit
	ExitGrace time.Duration  // how long to drain the sender before exiting; default 2s
}

// Manager owns the running RuntimeConfig and executes commands.
type Manager struct {
	opts Options

	cfg     atomic.Pointer[rxproto.RuntimeConfig]
	version atomic.Int64

	mu      sync.Mutex // serialises Apply and device registration
	dev     sdr.Device // the open dongle, nil while closed
	restart chan struct{}

	busy atomic.Bool // one update or capture at a time
}

// NewManager builds a Manager running opts.Config at opts.Version.
func NewManager(opts Options) *Manager {
	if opts.Exit == nil {
		opts.Exit = os.Exit
	}
	if opts.ExitGrace == 0 {
		opts.ExitGrace = 2 * time.Second
	}
	if opts.HTTP == nil {
		opts.HTTP = &http.Client{Timeout: 5 * time.Minute}
	}
	m := &Manager{opts: opts, restart: make(chan struct{}, 1)}
	cfg := opts.Config
	m.cfg.Store(&cfg)
	m.version.Store(int64(opts.Version))
	if opts.Level != nil {
		opts.Level.Set(LogLevel(cfg.LogLevel))
	}
	return m
}

// Config returns the running config.
func (m *Manager) Config() rxproto.RuntimeConfig { return *m.cfg.Load() }

// Version returns the applied config version (0 = env bootstrap/defaults).
func (m *Manager) Version() int { return int(m.version.Load()) }

// HelloConfig is the config block for the hello frame.
func (m *Manager) HelloConfig() rxproto.HelloConfig {
	return rxproto.HelloConfig{Version: m.Version(), Effective: m.Config()}
}

// RestartRequests is signalled when the dongle loop must close and reopen
// the device to pick up a config change or a dongle_reset command.
func (m *Manager) RestartRequests() <-chan struct{} { return m.restart }

// RegisterDevice tells the manager which device is open and which config
// it was opened with. If the config has moved on since the dongle loop
// read it, the difference is applied now (live) or a restart is
// requested, so no revision is lost in the gap between open and register.
func (m *Manager) RegisterDevice(dev sdr.Device, opened rxproto.RuntimeConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dev = dev
	// Drop a restart token left over from a device that has since closed.
	select {
	case <-m.restart:
	default:
	}
	cur := m.Config()
	if !rxproto.LiveApplicable(opened, cur) {
		m.requestRestartLocked()
		return
	}
	m.applyToDeviceLocked(opened, cur)
}

// UnregisterDevice tells the manager the device is closed.
func (m *Manager) UnregisterDevice() {
	m.mu.Lock()
	m.dev = nil
	m.mu.Unlock()
}

func (m *Manager) requestRestartLocked() {
	select {
	case m.restart <- struct{}{}:
	default:
	}
}

// applyToDeviceLocked pushes gain and ppm differences to the open device.
// A device that refuses is reopened with the full config instead.
func (m *Manager) applyToDeviceLocked(old, cur rxproto.RuntimeConfig) {
	if m.dev == nil {
		return
	}
	if old.TunerGainTenthsDB != cur.TunerGainTenthsDB {
		if err := m.dev.SetGain(cur.TunerGainTenthsDB); err != nil {
			slog.Warn("live gain change failed; restarting dongle", "gain_tenths_db", cur.TunerGainTenthsDB, "err", err)
			m.requestRestartLocked()
			return
		}
		slog.Info("tuner gain applied", "gain_tenths_db", cur.TunerGainTenthsDB, "agc", cur.TunerGainTenthsDB == 0)
	}
	if old.PPM != cur.PPM {
		if err := m.dev.SetPPM(cur.PPM); err != nil {
			slog.Warn("live ppm change failed; restarting dongle", "ppm", cur.PPM, "err", err)
			m.requestRestartLocked()
			return
		}
		slog.Info("frequency correction applied", "ppm", cur.PPM)
	}
}

// Apply validates and applies one config revision from the server, persists
// it, and reports the outcome (which is also sent via the Sender).
//
// Live: tuner gain, ppm, log level, stats interval and post_all. Anything
// else (sample rate, dongle selection, channels) is stored and the dongle
// loop is asked to restart so the next open uses it.
func (m *Manager) Apply(p rxproto.ConfigPayload) rxproto.ConfigResult {
	m.mu.Lock()
	defer m.mu.Unlock()

	old := m.Config()
	cfg := p.Config
	if errs := cfg.Validate(); errs != nil {
		slog.Warn("config revision rejected", "version", p.Version, "problems", errs)
		res := rxproto.ConfigResult{Version: p.Version, Status: rxproto.ConfigRejected, Errors: errs, Effective: old}
		m.send(res)
		return res
	}

	live := rxproto.LiveApplicable(old, cfg)
	m.cfg.Store(&cfg)
	m.version.Store(int64(p.Version))

	if m.opts.Level != nil {
		m.opts.Level.Set(LogLevel(cfg.LogLevel))
	}
	if m.opts.SetPostAll != nil && old.PostAll != cfg.PostAll {
		m.opts.SetPostAll(cfg.PostAll)
		if cfg.PostAll {
			slog.Warn("post_all enabled by server config — content filter bypassed; every decoded page will be posted upstream")
		}
	}
	if m.opts.Store != nil && m.opts.Store.Enabled() {
		if err := m.opts.Store.SaveConfig(p.Version, cfg); err != nil {
			slog.Warn("could not persist config", "version", p.Version, "err", err)
		}
	}

	if live {
		m.applyToDeviceLocked(old, cfg)
	} else if m.dev != nil {
		m.requestRestartLocked()
	}
	slog.Info("config revision applied", "version", p.Version, "live", live,
		"gain_tenths_db", cfg.TunerGainTenthsDB, "ppm", cfg.PPM, "sample_rate", cfg.SampleRate,
		"channels", len(cfg.EnabledChannels()), "log_level", cfg.LogLevel, "stats_interval_s", cfg.StatsIntervalS)

	res := rxproto.ConfigResult{Version: p.Version, Status: rxproto.ConfigApplied, Effective: cfg}
	m.send(res)
	return res
}

func (m *Manager) send(r rxproto.ConfigResult) {
	if m.opts.Sender != nil {
		m.opts.Sender.SendConfigResult(r)
	}
}

// Handle executes one command from the server and reports through the
// Sender. It blocks for the duration of the command (updates and captures
// take seconds); the websocket client runs it on its own goroutine.
func (m *Manager) Handle(cmd rxproto.Command) {
	slog.Info("command received", "id", cmd.ID, "type", cmd.Type)
	switch cmd.Type {
	case rxproto.CmdRestart:
		m.report(cmd, rxproto.CmdOK, "restarting", "exiting; systemd restarts the receiver")
		m.exit(0)
	case rxproto.CmdDongleReset:
		m.mu.Lock()
		m.requestRestartLocked()
		m.mu.Unlock()
		m.report(cmd, rxproto.CmdOK, "", "dongle loop restarting")
	case rxproto.CmdUpdate:
		if m.opts.Updater == nil {
			m.report(cmd, rxproto.CmdError, "", "self-update is not available on this receiver")
			return
		}
		if !m.busy.CompareAndSwap(false, true) {
			m.report(cmd, rxproto.CmdError, "", "another update or capture is in progress")
			return
		}
		defer m.busy.Store(false)
		var p rxproto.UpdateParams
		if err := json.Unmarshal(cmd.Params, &p); err != nil || p.Version == "" {
			m.report(cmd, rxproto.CmdError, "", "update needs a version")
			return
		}
		progress := func(stage, msg string) { m.report(cmd, rxproto.CmdProgress, stage, msg) }
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := m.opts.Updater.Run(ctx, cmd.ID, p.Version, progress); err != nil {
			slog.Error("update failed", "version", p.Version, "err", err)
			m.report(cmd, rxproto.CmdError, "", err.Error())
			return
		}
		// The ok result is sent after the first welcome from the new
		// binary (see ConfirmUpdate); here we just get out of the way.
		m.exit(0)
	case rxproto.CmdCapture:
		if !m.busy.CompareAndSwap(false, true) {
			m.report(cmd, rxproto.CmdError, "", "another update or capture is in progress")
			return
		}
		defer m.busy.Store(false)
		if err := m.capture(cmd); err != nil {
			slog.Warn("capture failed", "id", cmd.ID, "err", err)
			m.report(cmd, rxproto.CmdError, "", err.Error())
		}
	default:
		m.report(cmd, rxproto.CmdError, "", fmt.Sprintf("unknown command type %q", cmd.Type))
	}
}

func (m *Manager) report(cmd rxproto.Command, status, stage, msg string) {
	if m.opts.Sender == nil {
		return
	}
	m.opts.Sender.SendCommandResult(rxproto.CommandResult{ID: cmd.ID, Type: cmd.Type, Status: status, Stage: stage, Message: msg})
}

// exit drains the sender so the last result frame gets out, then exits.
func (m *Manager) exit(code int) {
	if m.opts.Sender != nil {
		ctx, cancel := context.WithTimeout(context.Background(), m.opts.ExitGrace)
		m.opts.Sender.Drain(ctx)
		cancel()
	}
	slog.Info("exiting on server command", "code", code)
	m.opts.Exit(code)
}

// ConfirmUpdate is called after the first welcome. If an update marker for
// the running version is present, the recorded command gets its ok result
// and the marker is removed; a later crash loop then no longer rolls back.
func (m *Manager) ConfirmUpdate(marker *UpdateMarker, version string) {
	if marker == nil || m.opts.Store == nil {
		return
	}
	if m.opts.Sender != nil {
		m.opts.Sender.SendCommandResult(rxproto.CommandResult{
			ID: marker.CommandID, Type: rxproto.CmdUpdate, Status: rxproto.CmdOK,
			Stage: "running", Message: "now running " + version,
		})
	}
	if err := m.opts.Store.DeleteMarker(); err != nil {
		slog.Warn("could not remove update marker", "err", err)
	}
	slog.Info("update confirmed", "from", marker.From, "to", marker.To, "version", version)
}

// LogLevel maps a RuntimeConfig log level to slog.
func LogLevel(name string) slog.Level {
	switch name {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}
