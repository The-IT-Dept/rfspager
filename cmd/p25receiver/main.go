// Command p25receiver monitors a P25 trunking control channel (the NSW PSN)
// with an RTL-SDR: site identity, channel plan, voice calls (and whether
// they're encrypted), affiliations and registrations. Everything it decodes
// is logged, and shipped to the rfspager server over the receiver websocket
// when P25_SERVER_URL / P25_API_KEY are set.
//
// It is deliberately separate from cmd/receiver: its own dongle, its own
// config (P25_* env, never /etc/rfspager/endpoints.json) and its own systems
// row, because the hub allows one socket per system and would otherwise
// evict a POCSAG receiver sharing the key.
//
//	go build -tags rtlsdr ./cmd/p25receiver
//	p25receiver -freqs 420.3,419.025 -serial 00000001
//	p25receiver -file internal/p25/testdata/cc_420.2983.cs8   # replay, no dongle
//
// Config (flags override env; /etc/rfspager/p25.env and ./.env are loaded
// if present):
//
//	P25_FREQS          control channels to hunt, MHz, comma separated
//	P25_DONGLE_SERIAL  dongle serial substring (default: index 0)
//	P25_PPM, P25_GAIN  correction (ppm), tuner gain (tenths of dB; 0 = AGC)
//	P25_SERVER_URL     e.g. https://rfspager.app
//	P25_API_KEY        the P25 receiver's own systems.api_key
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/the-it-dept/rfspager/internal/p25"
	"github.com/the-it-dept/rfspager/internal/receiver/dsp"
	"github.com/the-it-dept/rfspager/internal/receiver/sdr"
	"github.com/the-it-dept/rfspager/internal/receiver/wsclient"
)

const (
	inputRate = 2_400_000
	// tuneOffset keeps the control channel clear of the dongle's DC spike.
	tuneOffset = 200_000
	// lockTimeout: no valid frame for this long and we hunt the next
	// control channel. A live CC sends ~14 frames a second.
	lockTimeout = 5 * time.Second
	// dongleSilenceTimeout matches cmd/receiver: no IQ for this long means
	// the dongle has gone away.
	dongleSilenceTimeout = 10 * time.Second
)

func main() {
	_ = godotenv.Load("/etc/rfspager/p25.env")
	_ = godotenv.Load()

	freqs := flag.String("freqs", envOr("P25_FREQS", "420.3"), "control channels to hunt, MHz, comma separated")
	serial := flag.String("serial", os.Getenv("P25_DONGLE_SERIAL"), "dongle serial substring (default: index 0)")
	ppm := flag.Int("ppm", envInt("P25_PPM", 0), "dongle frequency correction, ppm")
	gain := flag.Int("gain", envInt("P25_GAIN", 400), "tuner gain, tenths of a dB (0 = AGC)")
	server := flag.String("server", os.Getenv("P25_SERVER_URL"), "rfspager server base URL")
	apiKey := flag.String("api-key", os.Getenv("P25_API_KEY"), "receiver API key")
	file := flag.String("file", "", "replay a 48 kS/s cs8 capture instead of a dongle")
	center := flag.Float64("center", 0, "dongle centre, MHz (default: control channel + 200 kHz); "+
		"set it between the control channel and the busiest voice channels to -record them")
	record := flag.String("record", "", "directory to write each in-band clear call's channel IQ (48 kS/s cf32)")
	voiceFile := flag.String("voice-file", "", "decode a -record capture (cf32) to WAV per slot in the current directory, then exit")
	afcHz := flag.Float64("afc", 0, "with -voice-file: AFC seed in Hz (the control channel decoder's estimate)")
	nacFlag := flag.String("nac", "2D2", "with -voice-file: site NAC (hex) for descrambling; WACN BEE00 / SysID 2D1 assumed")
	voice := flag.Bool("voice", envOr("P25_VOICE", "true") == "true", "decode and ship audio of in-band clear calls")
	flag.Parse()

	base := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(base))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var ws *wsclient.Client
	if *server != "" && *apiKey != "" {
		ws = wsclient.New(*server, *apiKey)
		slog.SetDefault(slog.New(wsclient.NewLogShippingHandler(base, ws)))
		go func() {
			if err := ws.Run(ctx); err != nil {
				slog.Error("ws transport stopped", "err", err)
			}
		}()
	}

	if *voiceFile != "" {
		nac, err := strconv.ParseUint(*nacFlag, 16, 16)
		if err != nil {
			fatal(err)
		}
		if err := decodeVoiceFile(*voiceFile, ".", 0xBEE00, 0x2D1, uint16(nac), *afcHz); err != nil {
			fatal(err)
		}
		return
	}

	t := newTally(ws)
	if *file != "" {
		iq, err := os.ReadFile(*file)
		if err != nil {
			fatal(err)
		}
		now := time.Now()
		rep := p25.NewReporter(t.send)
		var dec *p25.Decoder
		dec = p25.NewDecoder(func(m p25.Message) {
			rep.SetNAC(dec.Stats().NAC)
			rep.Handle(m, now)
		})
		dec.ProcessCS8(iq)
		rep.Reset(now) // ends open calls
		t.report(dec)
		return
	}

	list, err := parseFreqs(*freqs)
	if err != nil {
		fatal(err)
	}
	var rec *recorder
	if *record != "" {
		if err := os.MkdirAll(*record, 0o755); err != nil {
			fatal(err)
		}
		rec = newRecorder(*record)
		t.hooks = append(t.hooks, func(ft string, p any) {
			if c, ok := p.(p25.CallPayload); ok {
				rec.call(ft, c)
			}
		})
	}
	var fol *follower
	if *voice {
		fol = newFollower(t.send)
		t.hooks = append(t.hooks, func(ft string, p any) {
			switch v := p.(type) {
			case p25.CallPayload:
				fol.call(ft, v)
			case p25.SitePayload:
				fol.site = v
			}
		})
	}
	backoff := time.Second
	for ctx.Err() == nil {
		err := run(ctx, t, list, uint32(*center*1e6+0.5), rec, fol, *serial, *ppm, *gain)
		if ctx.Err() != nil {
			break
		}
		slog.Warn("dongle stopped, will retry", "err", err, "in", backoff)
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

// run opens the dongle and decodes until ctx ends or the dongle fails,
// hunting through freqs whenever the control channel is lost.
func run(ctx context.Context, t *tally, freqs []uint32, center uint32, rec *recorder, fol *follower, serial string, ppm, gain int) error {
	idx, err := dongleIndex(serial)
	if err != nil {
		return err
	}
	dev, err := sdr.Open(idx)
	if err != nil {
		return err
	}
	defer dev.Close()
	for _, step := range []error{dev.SetSampleRate(inputRate), dev.SetPPM(ppm), dev.SetGain(gain)} {
		if step != nil {
			return step
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	iq, err := dev.ReadIQ(ctx, 16*16384)
	if err != nil {
		return err
	}

	rep := p25.NewReporter(t.send)
	defer rep.Reset(time.Now())
	var (
		dec      *p25.Decoder
		ch       *p25.Channelizer
		cur      = -1
		lastLock time.Time
		frames   int
	)
	tune := func(i int) error {
		cur = i % len(freqs)
		cc := freqs[cur]
		tuned := cc + tuneOffset
		if off := int64(cc) - int64(center); center != 0 && abs(off) < recordSpan && abs(off) > 20_000 {
			tuned = center
		}
		if err := dev.Tune(tuned); err != nil {
			return err
		}
		rep.Reset(time.Now())
		if rec != nil {
			rec.retune(tuned)
		}
		ch = p25.NewChannelizer(float64(int64(cc)-int64(tuned)), inputRate)
		dec = p25.NewDecoder(func(m p25.Message) {
			rep.SetNAC(dec.Stats().NAC)
			rep.Handle(m, time.Now())
		})
		lastLock, frames = time.Now(), 0
		if fol != nil {
			fol.retune(tuned, cc, dec)
		}
		slog.Info("tuned", "dongle", dev.Name(), "freq_mhz", float64(cc)/1e6, "center_mhz", float64(tuned)/1e6)
		return nil
	}
	if err := tune(0); err != nil {
		return err
	}

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	nextReport := time.Now().Add(time.Minute)
	lastBuf := time.Now()
	var work []complex128
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-tick.C:
			if now.Sub(lastBuf) > dongleSilenceTimeout {
				return fmt.Errorf("dongle silent for %s", dongleSilenceTimeout)
			}
			if s := dec.Stats(); s.Frames != frames {
				frames, lastLock = s.Frames, now
			} else if now.Sub(lastLock) > lockTimeout && len(freqs) > 1 {
				slog.Warn("control channel lost, hunting", "freq_mhz", float64(freqs[cur])/1e6)
				if err := tune(cur + 1); err != nil {
					return err
				}
			}
			rep.Tick(now)
			if now.After(nextReport) {
				t.report(dec)
				nextReport = now.Add(5 * time.Minute)
			}
		case buf, ok := <-iq:
			if !ok {
				return fmt.Errorf("iq stream closed")
			}
			lastBuf = time.Now()
			work = dsp.IQToComplex(buf.IQ, work)
			sdr.Recycle(buf)
			dec.Process(ch.Process(work))
			if rec != nil {
				rec.process(work)
			}
			if fol != nil {
				fol.process(work, lastBuf)
			}
		}
	}
}

// tally logs and forwards every payload, and keeps running totals for the
// periodic stats line.
type tally struct {
	ws *wsclient.Client
	// hooks see every payload before it's sent, so the voice follower can
	// flush a call's last audio ahead of its call_end.
	hooks      []func(frameType string, payload any)
	site       string
	clear, enc int
	byGroup    map[uint16][2]int // clear, encrypted
	units      int
}

func newTally(ws *wsclient.Client) *tally { return &tally{ws: ws, byGroup: map[uint16][2]int{}} }

func (t *tally) send(frameType string, payload any) {
	for _, h := range t.hooks {
		h(frameType, payload)
	}
	if t.ws != nil {
		t.ws.Send(frameType, payload)
	}
	switch p := payload.(type) {
	case p25.SitePayload:
		site := fmt.Sprintf("%03X/%d/%d", p.SysID, p.RFSS, p.Site)
		if site != t.site {
			t.site = site
			slog.Info("site", "wacn", fmt.Sprintf("%05X", p.WACN), "sysid", fmt.Sprintf("%03X", p.SysID),
				"rfss", p.RFSS, "site", p.Site, "nac", fmt.Sprintf("%03X", p.NAC),
				"cc_mhz", float64(p.ControlHz)/1e6, "plan_entries", len(p.Plan), "neighbours", len(p.Neighbors))
		}
	case p25.CallPayload:
		if frameType == p25.FrameCallStart {
			slog.Info("call start", "tg", p.Group, "target", p.Target, "src", p.Source,
				"freq_mhz", float64(p.FreqHz)/1e6, "slot", p.Slot, "encrypted", p.Encrypted,
				"emergency", p.Emergency, "patch", p.Patch)
			return
		}
		g := t.byGroup[p.Group]
		if p.Encrypted {
			t.enc++
			g[1]++
		} else {
			t.clear++
			g[0]++
		}
		if p.Group != 0 {
			t.byGroup[p.Group] = g
		}
		slog.Info("call end", "tg", p.Group, "target", p.Target, "src", p.Source,
			"secs", float64(p.EndMS-p.StartMS)/1000, "encrypted", p.Encrypted)
	case p25.UnitPayload:
		t.units++
		slog.Info(p.Kind, "unit", p.Unit, "tg", p.Group, "result", p.Result)
	}
}

func (t *tally) report(dec *p25.Decoder) {
	s := dec.Stats()
	pct := 0.0
	if s.Blocks > 0 {
		pct = 100 * float64(s.BlocksOK) / float64(s.Blocks)
	}
	groups := make([]uint16, 0, len(t.byGroup))
	for g := range t.byGroup {
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool {
		a, b := t.byGroup[groups[i]], t.byGroup[groups[j]]
		return a[0]+a[1] > b[0]+b[1]
	})
	var top []string
	for _, g := range groups[:min(len(groups), 15)] {
		c := t.byGroup[g]
		top = append(top, fmt.Sprintf("%d:%dc/%de", g, c[0], c[1]))
	}
	slog.Info("stats", "site", t.site, "frames", s.Frames, "tsbk_ok_pct", fmt.Sprintf("%.1f", pct),
		"afc_hz", int(dec.FreqOffsetHz()), "calls_clear", t.clear, "calls_enc", t.enc,
		"unit_events", t.units, "top_groups", strings.Join(top, " "))
}

func dongleIndex(serial string) (int, error) {
	if serial == "" {
		return 0, nil
	}
	for _, d := range sdr.Enumerate() {
		if strings.Contains(strings.ToLower(d.Serial), strings.ToLower(serial)) {
			return d.Index, nil
		}
	}
	return 0, fmt.Errorf("no rtl-sdr dongle with serial matching %q", serial)
}

func parseFreqs(s string) ([]uint32, error) {
	var out []uint32
	for _, f := range strings.Split(s, ",") {
		mhz, err := strconv.ParseFloat(strings.TrimSpace(f), 64)
		if err != nil || mhz < 100 || mhz > 1000 {
			return nil, fmt.Errorf("bad control channel %q (want MHz, e.g. 420.3)", f)
		}
		out = append(out, uint32(mhz*1e6+0.5))
	}
	return out, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(k)); err == nil {
		return v
	}
	return def
}

func fatal(err error) {
	slog.Error("p25receiver", "err", err)
	os.Exit(1)
}
