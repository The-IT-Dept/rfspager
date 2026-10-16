// Command rxui-demo serves the receiver's local UI with synthetic pages,
// stats and logs, for working on apps/receiver-ui without a dongle:
//
//	go run ./cmd/rxui-demo            # UI + API on :8148
//	cd apps/receiver-ui && npm run dev  # Vite on :5173, proxies /api here
//
// It also runs a webhook sink on :8149 that logs every delivery and checks
// its signature against the secret "demo"; add http://localhost:8149/hook
// as a webhook to try it.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/the-it-dept/rfspager/internal/receiver/buildinfo"
	"github.com/the-it-dept/rfspager/internal/receiver/localui"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

var messages = []string{
	"RFS: GRASS FIRE - 12 MAIN RD BUDGEWOI - RESPOND CAT 1 - BUDGEWOI 1, 7 - MAP 46 C4",
	"RFS: MVA - PACIFIC HWY / WYEE RD WYEE - RESPOND CAT 1 - DORA CREEK 1",
	"FRNSW: AFA - WOOLWORTHS LAKE HAVEN - 333 LAKE HAVEN DR LAKE HAVEN - 205 (TOUKLEY)",
	"RFS: STRUCTURE FIRE - 4 BAY ST NORAVILLE - RESPOND CAT 1 - TOUKLEY 1, NORAH HEAD 1",
	"RFS: TREE DOWN - SPARKS RD WARNERVALE - RESPOND CAT 2 - WARNERVALE 1",
	"TEST PAGE PLEASE DISREGARD",
	"RFS: HAZARD REDUCTION - BLUE HAVEN - STAND DOWN - ALL UNITS",
}

var channels = []rxproto.ChannelConfig{
	{Label: "RFS", FreqHz: 148_587_500, Enabled: true},
	{Label: "FRNSW", FreqHz: 148_962_500, Enabled: true},
}

func main() {
	addr := flag.String("addr", ":8148", "UI listen address")
	sinkAddr := flag.String("sink", ":8149", "webhook sink listen address (empty = off)")
	history := flag.Duration("history", 6*time.Hour, "synthetic stats/pages to backfill")
	flag.Parse()

	logs := localui.NewLogBuffer(2000)
	slog.SetDefault(slog.New(logs.Handler(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))))

	dir, err := os.MkdirTemp("", "rxui-demo-")
	if err != nil {
		slog.Error("temp dir", "err", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg := rxproto.Defaults()
	cfg.Channels = channels
	bi := buildinfo.Info{Version: "5.9.0-demo", OS: "linux", Arch: "arm64", Hostname: "rfspager-demo-1"}
	started := time.Now()
	ui := localui.New(ctx, *addr, dir, bi, logs, localui.Sources{
		System:     func() *localui.System { return &localui.System{Name: "Budgewoi", UUID: "8947d028-e391-4c8a-b99f-31034ebfa384"} },
		DongleOpen: func() bool { return true },
		Host: func() rxproto.HostStats {
			return rxproto.HostStats{CPUTempC: 51 + rand.Float64()*3, UptimeS: int64(time.Since(started).Seconds()) + 273600, UndervoltageEver: true}
		},
		Endpoints: func() []localui.Endpoint {
			since := started.UnixMilli()
			last := time.Now().Add(-14 * time.Second).UnixMilli()
			errAt := time.Now().Add(-2 * time.Minute).UnixMilli()
			return []localui.Endpoint{
				{Name: "prod", Mode: "ws", URL: "https://rfspager.app", Primary: true, Connected: true,
					ConnectedSince: &since, RTTMs: 30 + rand.IntN(15), PagesSent: 412, LastPageAt: &last},
				{Name: "dev", Mode: "ws", URL: "https://go.dev.blah.au", Connected: false,
					ReconnectsTotal: 7, PagesSent: 98, PagesFailed: 3, LastError: "ws dial: connection refused", LastErrorAt: &errAt},
			}
		},
		Settings: func() localui.Settings {
			return localui.Settings{
				Config: cfg, ConfigVersion: 3, ConfigSource: "cache (/var/lib/rfspager/config.json)",
				Host:    rxproto.HostInfo{Model: "Raspberry Pi 4 Model B Rev 1.5", Kernel: "6.1.0-rpi7-rpi-v8", RTLSDR: "librtlsdr0 0.6.0-4"},
				Dongles: []rxproto.DongleInfo{{Index: 0, Name: "Generic RTL2832U OEM", Manufacturer: "Realtek", Product: "RTL2838UHIDIR", Serial: "00000001", Tuner: "R828D"}},
				Endpoints: []localui.EndpointInfo{
					{Name: "prod", Mode: "ws", URL: "https://rfspager.app", Primary: true},
					{Name: "dev", Mode: "ws", URL: "https://go.dev.blah.au"},
				},
				StateDir: "/var/lib/rfspager",
			}
		},
	})

	// Backfill so the charts and the page list are not empty on load.
	now := time.Now()
	for t := now.Add(-*history); t.Before(now); t = t.Add(15 * time.Second) {
		ui.Stats.Add(localui.StatsPoint{TS: t.UnixMilli(), Stats: fakeStats(t)})
	}
	for i := 40; i > 0; i-- {
		ui.Pages.Add(fakePage(now.Add(-time.Duration(i) * 7 * time.Minute)))
	}

	if *sinkAddr != "" {
		go runSink(*sinkAddr)
	}
	go ui.Run(ctx)
	slog.Info("demo running", "ui", "http://localhost"+*addr, "state_dir", dir)

	pageT := time.NewTimer(3 * time.Second)
	statsT := time.NewTicker(5 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return
		case <-pageT.C:
			p := fakePage(time.Now())
			slog.Info("page", "channel", p.Channel, "address", p.Address, "posted", p.Posted, "msg", p.Message)
			ui.AddPage(p)
			pageT.Reset(time.Duration(4+rand.IntN(12)) * time.Second)
		case t := <-statsT.C:
			st := fakeStats(t)
			st.IntervalS = 5
			ui.AddStats(st)
			if rand.IntN(4) == 0 {
				slog.Debug("stats sent", "channels", len(st.Channels))
			}
			if rand.IntN(10) == 0 {
				slog.Warn("iq stream gap; decoders resynced", "gaps", 1, "dropped_buffers", 2)
			}
		}
	}
}

func fakePage(t time.Time) localui.Page {
	ch := channels[rand.IntN(len(channels))]
	p := localui.Page{
		TS: t.UnixMilli(), Channel: ch.Label, FreqHz: ch.FreqHz, Baud: []int{512, 1200}[rand.IntN(2)], Function: 3,
		Address: fmt.Sprintf("0%06d", 521800+rand.IntN(80)), Posted: true,
	}
	if rand.IntN(5) == 0 {
		// What the content filter drops: a numeric beacon or noise.
		p.Posted = false
		p.Message = []string{"0180111", "U]E(-3", "WHC+darp\n98E\""}[rand.IntN(3)]
		p.Quality = &rxproto.PageQuality{Codewords: 2, Corrected: 1}
		return p
	}
	p.Message = messages[rand.IntN(len(messages))]
	q := &rxproto.PageQuality{Codewords: len(p.Message)*7/20 + 1}
	switch rand.IntN(6) {
	case 0:
		q.Corrected = 1 + rand.IntN(3)
	case 1:
		q.Uncorrectable = 1
		q.Damaged = [][2]int{{20, 23}}
	case 2:
		q.Truncated = true
	}
	p.Quality = q
	return p
}

func fakeStats(t time.Time) rxproto.Stats {
	phase := float64(t.Unix()%3600) / 3600 * 2 * math.Pi
	st := rxproto.Stats{
		IntervalS: 15,
		Dongle: rxproto.DongleStats{Open: true, AGC: true, CentreHz: 148_775_000, SampleRate: 1_920_000,
			IQBuffers: 1760, DSPLoad: 0.18 + 0.05*math.Sin(phase)},
		Host: rxproto.HostStats{CPUTempC: 50 + 4*math.Sin(phase), Load1: 0.4, MemFreeMB: 3100, UptimeS: 273600},
		Link: rxproto.LinkStats{RTTMs: 30 + rand.IntN(20)},
	}
	for i, c := range channels {
		busy := rand.IntN(3) == 0
		snr := 2 + rand.Float64()*2
		carrier := 0.0
		if busy {
			snr = 18 + float64(10-i*6) + rand.Float64()*6
			carrier = 5 + rand.Float64()*20
		}
		floor := -42 - float64(i)*3 + rand.Float64()
		clean := 0
		if busy {
			clean = 40 + rand.IntN(80)
		}
		st.Channels = append(st.Channels, rxproto.ChannelStats{
			Label: c.Label, FreqHz: c.FreqHz, NoiseFloorDBFS: floor, PowerDBFS: floor + snr, SNRDB: snr, CarrierPct: carrier,
			SyncLocks: clean / 20, CWClean: clean, CWCorrected: clean / 30, CWUncorrectable: rand.IntN(2) * clean / 60,
			Pages: clean / 40, LastPageAgeS: rand.IntN(120),
		})
	}
	return st
}

func runSink(addr string) {
	http.HandleFunc("/hook", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		sig := r.Header.Get("X-RFSPager-Signature")
		slog.Info("webhook sink received",
			"event", r.Header.Get("X-RFSPager-Event"),
			"signed", sig != "",
			"signature_ok_for_demo_secret", sig == localui.Sign("demo", body),
			"bytes", len(body),
		)
		w.WriteHeader(http.StatusNoContent)
	})
	http.HandleFunc("/fail", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	})
	slog.Info("webhook sink listening", "addr", addr, "ok", "/hook", "failing", "/fail")
	if err := http.ListenAndServe(addr, nil); err != nil {
		slog.Error("webhook sink stopped", "err", err)
	}
}
