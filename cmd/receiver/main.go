// Command receiver is a local RTL-SDR agent that listens on configured
// POCSAG frequencies, decodes pages (at 512, 1200, and 2400 baud), and
// forwards them to the rfspager server over an authenticated websocket
// (and, optionally, to legacy endpoints).
//
// One binary per host; runs on developer laptops, Raspberry Pis, and
// dedicated receiver boxes. Build with:
//
//	go build -tags rtlsdr ./cmd/receiver
//
// Bootstrap configuration (server, key, endpoints) comes from the
// environment (see internal/receiver/config). Radio tuning is a
// rxproto.RuntimeConfig owned by the server: the receiver runs the cached
// revision from the state dir, else the RECEIVER_* tuning variables, else
// the built-in defaults, and applies whatever the server pushes (see
// internal/receiver/control and docs/receiver-management.md).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/the-it-dept/rfspager/internal/receiver"
	"github.com/the-it-dept/rfspager/internal/receiver/buildinfo"
	"github.com/the-it-dept/rfspager/internal/receiver/config"
	"github.com/the-it-dept/rfspager/internal/receiver/control"
	"github.com/the-it-dept/rfspager/internal/receiver/dsp"
	"github.com/the-it-dept/rfspager/internal/receiver/legacy"
	"github.com/the-it-dept/rfspager/internal/receiver/localui"
	"github.com/the-it-dept/rfspager/internal/receiver/sdr"
	"github.com/the-it-dept/rfspager/internal/receiver/stats"
	"github.com/the-it-dept/rfspager/internal/receiver/wsclient"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// dongleSilenceTimeout: if the dongle stops delivering IQ buffers for this
// long we conclude it's been unplugged (or died) and tear down to retry.
// At 1.92 MS/s buffers arrive every ~8ms, so a 10-second gap is decisive.
const dongleSilenceTimeout = 10 * time.Second

// errDongleRestart is returned by runDongle when the manager asked for the
// device to be reopened (config change or dongle_reset); the outer loop
// reopens immediately instead of backing off.
var errDongleRestart = errors.New("dongle restart requested")

func main() {
	_ = godotenv.Load() // .env is optional
	level := &slog.LevelVar{}
	base := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	// Every record also lands in the local UI's log buffer, from the start.
	uiLogs := localui.NewLogBuffer(2000)
	slog.SetDefault(slog.New(uiLogs.Handler(base)))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config error", "err", err)
		os.Exit(2)
	}
	bi := buildinfo.Get()

	// State dir and the self-update marker come first: a binary that keeps
	// crashing after an update must be rolled back before it does anything.
	store := control.NewStore(config.StateDir())
	exe, err := control.ResolveExecutable()
	if err != nil {
		slog.Warn("cannot resolve own executable; self-update disabled", "err", err)
	}
	marker, rolledBack, err := control.BootCheck(store, exe, bi.Version)
	if err != nil {
		slog.Error("update marker check failed", "err", err)
	}
	if rolledBack {
		slog.Error("exiting so systemd starts the restored binary")
		os.Exit(1)
	}
	if marker != nil {
		// A build that stays up this long is not crash-looping; its later
		// restarts (dongle trouble, server outages) must not roll it back.
		time.AfterFunc(control.StableAfter, func() {
			if err := store.MarkStable(marker); err != nil {
				slog.Warn("could not mark the update stable", "err", err)
			}
		})
	}

	// Runtime config: cached server revision > env tuning > defaults.
	version, rt, source, problems := control.Startup(store, cfg.RuntimeBootstrap())
	if problems != nil {
		slog.Error("RECEIVER_* tuning values are invalid; running the built-in defaults", "problems", problems)
	}
	slog.Info("runtime config",
		"source", source, "version", version, "state_dir", store.Dir(),
		"gain_tenths_db", rt.TunerGainTenthsDB, "ppm", rt.PPM, "sample_rate", rt.SampleRate,
		"channels", len(rt.EnabledChannels()), "log_level", rt.LogLevel, "stats_interval_s", rt.StatsIntervalS,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Build one transport per configured endpoint. The first ws endpoint is
	// primary: it supplies the welcome (identity), carries shipped logs and
	// stats, and is the only one whose config and commands are honoured.
	// Additional ws endpoints and every legacy endpoint only receive pages.
	var (
		senders   []receiver.NamedSender
		primaryWS *wsclient.Client
		primaryEP string
		upstreams []upstream
	)
	for _, ep := range cfg.Endpoints {
		switch ep.Mode {
		case "ws":
			c := wsclient.New(ep.URL, ep.APIKey)
			if primaryWS == nil {
				primaryWS, primaryEP = c, ep.Name
				c.Primary = true
			}
			senders = append(senders, receiver.NamedSender{Name: ep.Name, Sender: c})
			upstreams = append(upstreams, upstream{ep: ep, ws: c})
		case "legacy":
			c := legacy.New(ep.URL, ep.SystemUUID)
			senders = append(senders, receiver.NamedSender{Name: ep.Name, Sender: c})
			upstreams = append(upstreams, upstream{ep: ep, legacy: c})
		}
	}

	// Mirror every slog record to the primary WS as well as stdout. The hub
	// persists them in system_logs (7-day retention) and fans them out to owner
	// + admin viewers in real time. The WS client drops log frames if its send
	// buffer is full, so this can't block the decode hot path. A legacy-only
	// receiver has nowhere to ship logs, so it just writes to stdout.
	if primaryWS != nil {
		slog.SetDefault(slog.New(uiLogs.Handler(wsclient.NewLogShippingHandler(base, primaryWS))))
	}

	for _, ep := range cfg.Endpoints {
		slog.Info("endpoint configured",
			"name", ep.Name, "mode", ep.Mode, "url", ep.URL,
			"primary", ep.Mode == "ws" && ep.Name == primaryEP,
		)
	}

	multi := receiver.NewMultiSender(senders)
	slog.Info("page fan-out ready", "endpoints", multi.Names())
	fwd := receiver.NewForwarder(multi, rt.PostAll)
	if rt.PostAll {
		slog.Warn("post_all is on — content filter bypassed; every decoded page will be posted upstream")
	}

	pipe := &pipeline{}
	opts := control.Options{
		Store: store, Version: version, Config: rt,
		Level: level, SetPostAll: fwd.SetPostAll, Taps: pipe,
	}
	if primaryWS != nil {
		opts.Sender = primaryWS
		opts.ServerURL = primaryWS.ServerURL()
		opts.APIKey = primaryWS.APIKey()
		opts.Updater = &control.Updater{
			ServerURL: primaryWS.ServerURL(), APIKey: primaryWS.APIKey(),
			Store: store, Version: bi.Version, Executable: exe,
		}
	}
	mgr := control.NewManager(opts)

	// The host's own view of this receiver, on their network. Off with
	// RECEIVER_UI_ADDR=off; a port clash is logged and decoding carries on.
	var system atomic.Pointer[localui.System]
	var ui *localui.Server
	if addr := config.UIAddr(); addr != "" {
		ui = localui.New(ctx, addr, store.Dir(), bi, uiLogs, uiSources(mgr, pipe, upstreams, primaryWS, &system, version, source))
		fwd.Observe = func(p receiver.DecodedPage) {
			ui.AddPage(localui.Page{
				TS: p.Time.UnixMilli(), Channel: p.Channel, FreqHz: p.FreqHz, Baud: p.Baud,
				Address: p.Address, Function: p.Function, Message: p.Message,
				Posted: p.Posted, Quality: p.Quality,
			})
		}
		go ui.Run(ctx)
	} else {
		slog.Info("local ui disabled (RECEIVER_UI_ADDR=off)")
	}

	if primaryWS != nil {
		primaryWS.HostInfo = stats.Info
		primaryWS.ConfigInfo = mgr.HelloConfig
		primaryWS.OnConfig = func(p rxproto.ConfigPayload) {
			// Every reconnect replays the current revision in the welcome;
			// don't re-apply and re-report what is already running.
			if p.Version != 0 && p.Version == mgr.Version() {
				slog.Debug("config revision already running", "version", p.Version)
				return
			}
			mgr.Apply(p)
		}
		primaryWS.OnCommand = mgr.Handle
		var confirmOnce sync.Once
		primaryWS.OnWelcome = func(w rxproto.Welcome) {
			system.Store(&localui.System{Name: w.Name, UUID: w.SystemUUID})
			confirmOnce.Do(func() { mgr.ConfirmUpdate(marker, bi.Version) })
			if w.LatestVersion != "" && control.BaseVersion(w.LatestVersion) != control.BaseVersion(bi.Version) {
				slog.Info("a different receiver release is published", "running", bi.Version, "latest", w.LatestVersion)
			}
		}
	}

	wsDone := make(chan struct{})
	if primaryWS != nil {
		go func() {
			defer close(wsDone)
			if err := primaryWS.Run(ctx); err != nil {
				slog.Error("ws transport stopped", "err", err)
			}
		}()
	} else {
		close(wsDone)
	}

	// Secondary ws endpoints get their own transport goroutine; a failure on one
	// must not stop the others or the receiver.
	for _, ns := range senders {
		c, ok := ns.Sender.(*wsclient.Client)
		if !ok || c == primaryWS {
			continue
		}
		name := ns.Name
		go func() {
			if err := c.Run(ctx); err != nil {
				slog.Error("ws transport stopped", "endpoint", name, "err", err)
			}
		}()
	}

	if primaryWS != nil {
		welcomeCtx, welcomeCancel := context.WithTimeout(ctx, 15*time.Second)
		defer welcomeCancel()
		welcome, err := primaryWS.Welcomed(welcomeCtx)
		if err != nil {
			// Not fatal: decode with the cached config and keep dialling in
			// the background. Exiting here would count as a crashed boot
			// right after an update and could roll back a good binary.
			slog.Warn("no welcome from the server yet; decoding with the local config while reconnecting",
				"endpoint", primaryEP, "err", err)
		} else {
			slog.Info("authenticated",
				"endpoint", primaryEP,
				"system", welcome.Name,
				"system_id", welcome.SystemUUID,
				"config_version", mgr.Version(),
			)
		}
	} else {
		slog.Info("no ws endpoint configured; running legacy-only")
	}
	go statsLoop(ctx, mgr, pipe, primaryWS, ui)

	// Outer retry loop: open the dongle, run until it dies (unplugged, USB
	// error, watchdog timeout) or the manager asks for a reopen, then try
	// again. Designed to survive both "no dongle at startup" and hot-unplug
	// mid-flight.
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for {
		if ctx.Err() != nil {
			slog.Info("shutting down")
			return
		}
		if sdr.Count() == 0 {
			slog.Warn("no rtl-sdr dongles detected; retrying", "in", backoff)
			sleepCtx(ctx, backoff)
			backoff = bumpBackoff(backoff, maxBackoff)
			continue
		}
		err := runDongle(ctx, mgr, pipe, fwd)
		if err == nil || errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return
		}
		if errors.Is(err, errDongleRestart) {
			slog.Info("reopening dongle", "config_version", mgr.Version())
			backoff = time.Second
			continue
		}
		slog.Warn("dongle stopped, will retry", "err", err, "in", backoff)
		sleepCtx(ctx, backoff)
		backoff = bumpBackoff(backoff, maxBackoff)
	}
}

// pipeline is what the dongle loop shares with the stats loop and the
// control manager: the live channels, the open flag, the dongle counters
// and the spectrum tap. Channels are rebuilt on every dongle open from the
// config current at that moment; the last set is kept while the dongle is
// closed so stats keep listing them.
type pipeline struct {
	mu       sync.Mutex
	channels []*receiver.Channel
	open     atomic.Bool
	spectrum atomic.Pointer[receiver.Spectrum]
	dongle   stats.Dongle
}

func (p *pipeline) setChannels(chs []*receiver.Channel) {
	p.mu.Lock()
	p.channels = chs
	p.mu.Unlock()
}

// Channels returns the current channel set.
func (p *pipeline) Channels() []*receiver.Channel {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*receiver.Channel{}, p.channels...)
}

// Channel implements control.Taps.
func (p *pipeline) Channel(label string) *receiver.Channel {
	for _, ch := range p.Channels() {
		if ch.Label == label {
			return ch
		}
	}
	return nil
}

// Spectrum implements control.Taps.
func (p *pipeline) Spectrum(s *receiver.Spectrum) error {
	if !p.open.Load() {
		return errors.New("the dongle is not open")
	}
	if !p.spectrum.CompareAndSwap(nil, s) {
		return errors.New("a spectrum capture is already in progress")
	}
	return nil
}

// CancelSpectrum implements control.Taps.
func (p *pipeline) CancelSpectrum(s *receiver.Spectrum) { p.spectrum.CompareAndSwap(s, nil) }

// statsLoop collects a stats frame every stats_interval_s (re-read each
// round, so a config change takes effect after the current wait) and hands
// it to the primary endpoint and the local UI, either of which may be nil.
// Channel counters are deltas reset by each collection, so this is their
// only reader.
func statsLoop(ctx context.Context, mgr *control.Manager, pipe *pipeline, ws *wsclient.Client, ui *localui.Server) {
	if ws == nil && ui == nil {
		return
	}
	for {
		interval := mgr.Config().StatsIntervalS
		if interval < rxproto.MinStatsIntervalS {
			interval = rxproto.Defaults().StatsIntervalS
		}
		t := time.NewTimer(time.Duration(interval) * time.Second)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		st := collectStats(mgr, pipe, ws, interval)
		if ws != nil {
			ws.SendStats(st)
		}
		if ui != nil {
			ui.AddStats(st)
		}
	}
}

func collectStats(mgr *control.Manager, pipe *pipeline, ws *wsclient.Client, interval int) rxproto.Stats {
	rt := mgr.Config()
	centre, _ := rt.CentreHz()
	buffers, dropped, gaps, load := pipe.dongle.Snapshot(rt.SampleRate)
	st := rxproto.Stats{
		IntervalS: interval,
		Dongle: rxproto.DongleStats{
			Open:         pipe.open.Load(),
			GainTenthsDB: rt.TunerGainTenthsDB,
			AGC:          rt.TunerGainTenthsDB == 0,
			PPM:          rt.PPM,
			CentreHz:     centre,
			SampleRate:   rt.SampleRate,
			IQBuffers:    buffers,
			IQDropped:    dropped,
			Gaps:         gaps,
			DSPLoad:      load,
		},
		Host:     stats.Host(),
		Channels: []rxproto.ChannelStats{},
	}
	if ws != nil {
		st.Link = ws.LinkStats()
	}
	for _, ch := range pipe.Channels() {
		st.Channels = append(st.Channels, ch.Snapshot())
	}
	return st
}

// runDongle opens the SDR with the current config, builds the channels,
// and pumps IQ buffers into them until the dongle stops delivering
// samples, the manager asks for a reopen, or ctx is cancelled. Returns nil
// only on a clean shutdown via ctx; otherwise an error describing why the
// SDR loop ended.
func runDongle(ctx context.Context, mgr *control.Manager, pipe *pipeline, fwd *receiver.Forwarder) error {
	rt := mgr.Config()
	// The config was validated when it was applied, but the env bootstrap
	// and the cache are only trusted after the same check: this is where
	// "channels span too wide for the sample rate" is caught.
	if errs := rt.Validate(); errs != nil {
		return fmt.Errorf("runtime config invalid: %v", errs)
	}
	centre, ok := rt.CentreHz()
	if !ok {
		return errors.New("no channels enabled")
	}
	slog.Info("dongle tuning",
		"centre_mhz", fmt.Sprintf("%.4f", float64(centre)/1e6),
		"sample_rate", rt.SampleRate,
		"channels", len(rt.EnabledChannels()),
		"bauds", receiver.SupportedBauds,
		"gain_tenths_db", rt.TunerGainTenthsDB,
		"ppm", rt.PPM,
	)

	dongleIdx, err := resolveDongleIndex(rt)
	if err != nil {
		return err
	}
	dev, err := sdr.Open(dongleIdx)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer dev.Close()
	slog.Info("dongle opened", "index", dev.Index(), "name", dev.Name())

	if err := dev.SetSampleRate(rt.SampleRate); err != nil {
		return fmt.Errorf("set sample rate: %w", err)
	}
	if err := dev.SetPPM(rt.PPM); err != nil {
		return fmt.Errorf("set ppm: %w", err)
	}
	if err := dev.SetGain(rt.TunerGainTenthsDB); err != nil {
		return fmt.Errorf("set gain: %w", err)
	}
	if err := dev.Tune(centre); err != nil {
		return fmt.Errorf("tune: %w", err)
	}

	sdrCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// One channel pipeline per enabled frequency, with a forwarder each,
	// both scoped to this dongle session.
	var channels []*receiver.Channel
	for _, c := range rt.EnabledChannels() {
		offset := float64(c.FreqHz) - float64(centre)
		ch := receiver.NewChannel(c.Label, c.FreqHz, offset, rt.SampleRate, rxproto.AudioRate)
		channels = append(channels, ch)
		go fwd.Run(sdrCtx, ch)
		slog.Info("channel ready",
			"label", c.Label,
			"freq_mhz", float64(c.FreqHz)/1e6,
			"offset_khz", offset/1000,
		)
	}
	pipe.setChannels(channels)
	pipe.open.Store(true)
	defer pipe.open.Store(false)
	defer pipe.spectrum.Store(nil)

	// Register after configuring: a revision applied in the meantime is
	// pushed to the device now or triggers an immediate reopen.
	mgr.RegisterDevice(dev, rt)
	defer mgr.UnregisterDevice()

	iqStream, err := dev.ReadIQ(sdrCtx, 16384*2)
	if err != nil {
		return fmt.Errorf("start iq read: %w", err)
	}

	// Watchdog: librtlsdr goes silent (no error, no closed channel) when a
	// dongle is unplugged, so we infer death from buffer drought.
	var lastBufNanos atomic.Int64
	lastBufNanos.Store(time.Now().UnixNano())
	go func() {
		t := time.NewTicker(dongleSilenceTimeout / 2)
		defer t.Stop()
		for {
			select {
			case <-sdrCtx.Done():
				return
			case <-t.C:
				if time.Since(time.Unix(0, lastBufNanos.Load())) > dongleSilenceTimeout {
					slog.Warn("dongle silent, assuming disconnect")
					cancel()
					return
				}
			}
		}
	}()

	var (
		work         []complex128
		gaps         int       // stream gaps since the last gap log line
		gapsDropped  int       // buffers lost across those gaps
		nextGapLogAt time.Time // rate-limit gap logging to one line per 10s
	)
	for {
		select {
		case <-sdrCtx.Done():
			if ctx.Err() != nil {
				return nil // clean shutdown from parent
			}
			return errors.New("watchdog tripped or sdr cancelled")
		case <-mgr.RestartRequests():
			return errDongleRestart
		case buf, ok := <-iqStream:
			if !ok {
				return errors.New("iq stream closed")
			}
			lastBufNanos.Store(time.Now().UnixNano())
			if buf.DroppedBefore > 0 {
				// The SDR layer shed buffers because we fell behind, so the
				// sample stream has a hole of unknown bit-length. Finalise
				// in-flight pages and hunt for sync again instead of
				// decoding the misaligned remainder as garbage.
				for _, ch := range channels {
					ch.Gap()
				}
				gaps++
				gapsDropped += buf.DroppedBefore
				if now := time.Now(); now.After(nextGapLogAt) {
					slog.Warn("iq stream gap; decoders resynced",
						"gaps", gaps,
						"dropped_buffers", gapsDropped,
					)
					gaps, gapsDropped = 0, 0
					nextGapLogAt = now.Add(10 * time.Second)
				}
			}
			work = dsp.IQToComplex(buf.IQ, work)
			dropped := buf.DroppedBefore
			sdr.Recycle(buf)
			started := time.Now()
			for _, ch := range channels {
				ch.Process(work)
			}
			pipe.dongle.Buffer(dropped, len(work), time.Since(started))
			if sp := pipe.spectrum.Load(); sp != nil && sp.Feed(work) {
				pipe.spectrum.CompareAndSwap(sp, nil)
			}
		}
	}
}

// sleepCtx waits for d or for ctx to cancel, whichever is sooner.
func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func bumpBackoff(cur, ceiling time.Duration) time.Duration {
	next := cur * 2
	if next > ceiling {
		return ceiling
	}
	return next
}

// resolveDongleIndex picks which RTL-SDR dongle to open based on config.
// Priority: dongle_serial (case-insensitive substring match) over
// dongle_index. Returns an error if a serial is configured but no
// connected dongle matches it — failing loudly is better than silently
// falling back to the wrong dongle.
func resolveDongleIndex(rt rxproto.RuntimeConfig) (int, error) {
	if rt.DongleSerial != "" {
		want := strings.ToLower(rt.DongleSerial)
		for _, d := range sdr.Enumerate() {
			if strings.Contains(strings.ToLower(d.Serial), want) {
				slog.Info("dongle selected by serial",
					"serial", d.Serial,
					"index", d.Index,
					"name", d.Name,
				)
				return d.Index, nil
			}
		}
		return 0, fmt.Errorf("no rtl-sdr dongle with serial matching %q", rt.DongleSerial)
	}
	return max(rt.DongleIndex, 0), nil
}
