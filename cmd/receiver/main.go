// Command receiver is a local RTL-SDR agent that listens on configured
// POCSAG frequencies, decodes pages (at 512, 1200, and 2400 baud), and
// forwards them to the rfspager server's Pagermon-compatible ingress
// endpoint.
//
// One binary per host; runs on developer laptops, Raspberry Pis, and
// dedicated receiver boxes. Build with:
//
//	go build -tags rtlsdr ./cmd/receiver
//
// Configuration is via environment variables (see internal/receiver/config).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/the-it-dept/rfspager/internal/receiver"
	"github.com/the-it-dept/rfspager/internal/receiver/config"
	"github.com/the-it-dept/rfspager/internal/receiver/dsp"
	"github.com/the-it-dept/rfspager/internal/receiver/sdr"
	"github.com/the-it-dept/rfspager/internal/receiver/spectrum"
	"github.com/the-it-dept/rfspager/internal/receiver/wsclient"
)

// spectrumInterval throttles waterfall frames pushed to the server.
const spectrumInterval = 200 * time.Millisecond

// dongleSilenceTimeout: if the dongle stops delivering IQ buffers for this
// long we conclude it's been unplugged (or died) and tear down to retry.
// At 1.92 MS/s buffers arrive every ~8ms, so a 10-second gap is decisive.
const dongleSilenceTimeout = 10 * time.Second

// channel is one POCSAG frequency the receiver listens on. Hardcoded for
// now while we only have two systems; lift to config when that changes.
type channel struct {
	label  string
	freqHz uint32
}

var hardcodedChannels = []channel{
	{label: "RFS", freqHz: 148_587_500},
	{label: "FRNSW", freqHz: 148_962_500},
}

// audioRate is the per-channel sample rate after decimation. 38.4 kHz is an
// integer multiple of 1200/2400/512 and divides 1.92 MS/s cleanly.
const audioRate uint32 = 38_400

func main() {
	_ = godotenv.Load() // .env is optional
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config error", "err", err)
		os.Exit(2)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	ws := wsclient.New(cfg.ServerURL, cfg.APIKey)

	// From this point on, mirror every slog record to the WS as well as
	// stdout. The hub persists them in system_logs (7-day retention) and
	// fans them out to owner + admin viewers in real time. The WS client
	// drops log frames if its send buffer is full, so this can't block
	// the decode hot path.
	base := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(wsclient.NewLogShippingHandler(base, ws)))

	wsDone := make(chan struct{})
	go func() {
		defer close(wsDone)
		if err := ws.Run(ctx); err != nil {
			slog.Error("ws transport stopped", "err", err)
		}
	}()
	welcomeCtx, welcomeCancel := context.WithTimeout(ctx, 15*time.Second)
	defer welcomeCancel()
	welcome, err := ws.Welcomed(welcomeCtx)
	if err != nil {
		slog.Error("ws welcome timed out", "key", maskKey(cfg.APIKey), "err", err)
		os.Exit(2)
	}
	slog.Info("authenticated",
		"system", welcome.Name,
		"system_id", welcome.SystemUUID,
	)
	var sender receiver.PageSender = ws

	// Pick dongle centre: midpoint of the lowest and highest hardcoded freqs.
	var lo, hi uint32 = hardcodedChannels[0].freqHz, hardcodedChannels[0].freqHz
	for _, c := range hardcodedChannels {
		if c.freqHz < lo {
			lo = c.freqHz
		}
		if c.freqHz > hi {
			hi = c.freqHz
		}
	}
	centre := (lo + hi) / 2
	const guardHz = 50_000
	maxOffset := float64(cfg.SampleRate)/2 - guardHz
	if math.Abs(float64(hi)-float64(centre)) > maxOffset {
		slog.Error("channels span too wide for sample rate",
			"low_mhz", float64(lo)/1e6,
			"high_mhz", float64(hi)/1e6,
			"sample_rate", cfg.SampleRate,
		)
		os.Exit(2)
	}
	slog.Info("dongle tuning",
		"centre_mhz", fmt.Sprintf("%.4f", float64(centre)/1e6),
		"sample_rate", cfg.SampleRate,
		"channels", len(hardcodedChannels),
		"bauds", receiver.SupportedBauds,
	)

	// Spin up one channel pipeline per configured frequency. Channels and
	// forwarders are long-lived: they survive dongle hot-unplug/replug
	// without disturbing the upstream transport or losing decoder state.
	fwd := &receiver.Forwarder{Sender: sender, PostAll: cfg.PostAll}
	if cfg.PostAll {
		slog.Warn("RECEIVER_POST_ALL=1 — content filter bypassed; every decoded page will be posted upstream")
	}
	var channels []*receiver.Channel
	for _, c := range hardcodedChannels {
		offset := float64(c.freqHz) - float64(centre)
		ch := receiver.NewChannel(c.label, c.freqHz, offset, cfg.SampleRate, audioRate)
		channels = append(channels, ch)
		go fwd.Run(ctx, ch)
		slog.Info("channel ready",
			"label", c.label,
			"freq_mhz", float64(c.freqHz)/1e6,
			"offset_khz", offset/1000,
		)
	}

	// Outer retry loop: open the dongle, run until it dies (unplugged, USB
	// error, watchdog timeout), then back off and try again. Designed to
	// survive both "no dongle at startup" and hot-unplug mid-flight.
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
		err := runDongle(ctx, cfg, centre, channels, sender)
		if err == nil || errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return
		}
		slog.Warn("dongle stopped, will retry", "err", err, "in", backoff)
		sleepCtx(ctx, backoff)
		backoff = bumpBackoff(backoff, maxBackoff)
	}
}

// runDongle opens the SDR, configures + tunes it, and pumps IQ buffers into
// each Channel until the dongle stops delivering samples or ctx is cancelled.
// Returns nil only on a clean shutdown via ctx; otherwise an error describing
// why the SDR loop ended.
//
// Spectrum frames share the authenticated WebSocket transport.
func runDongle(ctx context.Context, cfg *config.Config, centre uint32, channels []*receiver.Channel, sender receiver.PageSender) error {
	ws, _ := sender.(*wsclient.Client) // fake PageSenders may omit spectrum in tests
	dongleIdx, err := resolveDongleIndex(cfg)
	if err != nil {
		return err
	}
	dev, err := sdr.Open(dongleIdx)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer dev.Close()
	slog.Info("dongle opened", "index", dev.Index(), "name", dev.Name())

	if err := dev.SetSampleRate(cfg.SampleRate); err != nil {
		return fmt.Errorf("set sample rate: %w", err)
	}
	if err := dev.SetPPM(cfg.CorrectionPPM); err != nil {
		return fmt.Errorf("set ppm: %w", err)
	}
	if err := dev.SetGain(cfg.TunerGain); err != nil {
		return fmt.Errorf("set gain: %w", err)
	}
	if err := dev.Tune(centre); err != nil {
		return fmt.Errorf("tune: %w", err)
	}

	sdrCtx, cancel := context.WithCancel(ctx)
	defer cancel()

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
		work           []complex128
		nextSpectrumAt = time.Now()
		gaps           int       // stream gaps since the last gap log line
		gapsDropped    int       // buffers lost across those gaps
		nextGapLogAt   time.Time // rate-limit gap logging to one line per 10s
	)
	for {
		select {
		case <-sdrCtx.Done():
			if ctx.Err() != nil {
				return nil // clean shutdown from parent
			}
			return errors.New("watchdog tripped or sdr cancelled")
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
			sdr.Recycle(buf)
			for _, ch := range channels {
				ch.Process(work)
			}
			// Compute and send a spectrum slice at most once per
			// spectrumInterval. Disabled via RECEIVER_DISABLE_SPECTRUM=1 for
			// debugging USB stability on marginal hardware.
			if ws != nil && !cfg.DisableSpectrum {
				if now := time.Now(); now.After(nextSpectrumAt) && len(work) >= spectrum.FFTSize {
					if bins := spectrum.Compute(work); bins != nil {
						ws.SendSpectrum(centre, cfg.SampleRate, bins)
					}
					nextSpectrumAt = now.Add(spectrumInterval)
				}
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
// Priority: RECEIVER_DONGLE_SERIAL (case-insensitive substring match) over
// RECEIVER_DONGLE_INDEX. Returns an error if a serial is configured but no
// connected dongle matches it — failing loudly is better than silently
// falling back to the wrong dongle.
func resolveDongleIndex(cfg *config.Config) (int, error) {
	if cfg.DongleSerial != "" {
		want := strings.ToLower(cfg.DongleSerial)
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
		return 0, fmt.Errorf("no rtl-sdr dongle with serial matching %q", cfg.DongleSerial)
	}
	return max(cfg.DongleIndex, 0), nil
}

func maskKey(k string) string {
	if len(k) <= 6 {
		return "***"
	}
	return k[:3] + "..." + k[len(k)-3:]
}
