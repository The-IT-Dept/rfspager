package control

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/the-it-dept/rfspager/internal/receiver"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// fakeTaps owns one channel and feeds it (and any spectrum) with a steady
// synthetic tone from a goroutine, like the dongle loop would.
type fakeTaps struct {
	ch       *receiver.Channel
	mu       sync.Mutex
	spectrum *receiver.Spectrum
	open     bool
	stop     chan struct{}
}

func newFakeTaps(t *testing.T) *fakeTaps {
	const rate = 1_920_000
	ft := &fakeTaps{
		ch:   receiver.NewChannel("RFS", 148_587_500, -187_500, rate, rxproto.AudioRate),
		open: true,
		stop: make(chan struct{}),
	}
	go func() {
		for range ft.ch.Pages() {
		}
	}()
	go func() {
		block := make([]complex128, 16384)
		var phase float64
		t := time.NewTicker(2 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-ft.stop:
				return
			case <-t.C:
			}
			for i := range block {
				block[i] = complex(0.3*math.Cos(phase), 0.3*math.Sin(phase))
				phase += 2 * math.Pi * -187_500 / rate
			}
			ft.ch.Process(block)
			ft.mu.Lock()
			sp := ft.spectrum
			ft.mu.Unlock()
			if sp != nil && sp.Feed(block) {
				ft.mu.Lock()
				ft.spectrum = nil
				ft.mu.Unlock()
			}
		}
	}()
	t.Cleanup(func() { close(ft.stop) })
	return ft
}

func (f *fakeTaps) Channel(label string) *receiver.Channel {
	if label == f.ch.Label {
		return f.ch
	}
	return nil
}

func (f *fakeTaps) Spectrum(s *receiver.Spectrum) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.open {
		return errors.New("dongle closed")
	}
	if f.spectrum != nil {
		return errors.New("busy")
	}
	f.spectrum = s
	return nil
}

func (f *fakeTaps) CancelSpectrum(s *receiver.Spectrum) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.spectrum == s {
		f.spectrum = nil
	}
}

type upload struct {
	id, kind, rate, channel, contentType string
	body                                 []byte
}

func newUploadServer(t *testing.T, got chan upload) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.Header.Get("apikey") != "KEY" {
			http.Error(w, "nope", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		got <- upload{
			id: r.URL.Path, kind: r.Header.Get("X-Capture-Kind"), rate: r.Header.Get("X-Capture-Rate"),
			channel: r.Header.Get("X-Capture-Channel"), contentType: r.Header.Get("Content-Type"), body: body,
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func stages(res []rxproto.CommandResult) []string {
	var out []string
	for _, r := range res {
		out = append(out, r.Status+":"+r.Stage)
	}
	return out
}

func TestCaptureAudioUploads(t *testing.T) {
	got := make(chan upload, 1)
	srv := newUploadServer(t, got)
	taps := newFakeTaps(t)
	sender := &fakeSender{}
	m := NewManager(Options{Config: rxproto.Defaults(), Sender: sender, Taps: taps, ServerURL: srv.URL, APIKey: "KEY"})

	m.Handle(rxproto.Command{ID: "cap1", Type: rxproto.CmdCapture, Params: json.RawMessage(`{"capture_id":"abc","kind":"audio","channel":"RFS","seconds":1}`)})
	select {
	case u := <-got:
		if u.id != "/api/receiver/captures/abc" || u.kind != "audio" || u.rate != "38400" || u.channel != "RFS" || u.contentType != "application/octet-stream" {
			t.Errorf("upload headers: %+v", u)
		}
		if len(u.body) != 38400*2 {
			t.Errorf("body %d bytes, want %d", len(u.body), 38400*2)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no upload")
	}
	res := sender.commandResults()
	want := []string{"progress:recording", "progress:uploading", "ok:"}
	if s := stages(res); len(s) != 3 || s[0] != want[0] || s[1] != want[1] || s[2] != want[2] {
		t.Errorf("results %v, want %v", s, want)
	}
}

func TestCaptureIQAndSpectrumUpload(t *testing.T) {
	got := make(chan upload, 2)
	srv := newUploadServer(t, got)
	taps := newFakeTaps(t)
	sender := &fakeSender{}
	m := NewManager(Options{Config: rxproto.Defaults(), Sender: sender, Taps: taps, ServerURL: srv.URL, APIKey: "KEY"})

	m.Handle(rxproto.Command{ID: "cap2", Type: rxproto.CmdCapture, Params: json.RawMessage(`{"capture_id":"iq1","kind":"iq","channel":"RFS","seconds":1}`)})
	select {
	case u := <-got:
		if u.kind != "iq" || u.rate != "38400" || len(u.body) != 38400*4 {
			t.Errorf("iq upload: kind=%s rate=%s bytes=%d", u.kind, u.rate, len(u.body))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no iq upload")
	}

	m.Handle(rxproto.Command{ID: "cap3", Type: rxproto.CmdCapture, Params: json.RawMessage(`{"capture_id":"sp1","kind":"spectrum"}`)})
	select {
	case u := <-got:
		if u.kind != "spectrum" || u.rate != "1920000" || u.contentType != "application/json" || u.channel != "" {
			t.Errorf("spectrum upload: %+v", u)
		}
		var sp spectrumJSON
		if err := json.Unmarshal(u.body, &sp); err != nil {
			t.Fatal(err)
		}
		if sp.CentreHz != 148_775_000 || sp.SampleRate != 1_920_000 || len(sp.BinsDB) != SpectrumBins {
			t.Errorf("spectrum json: centre=%d rate=%d bins=%d", sp.CentreHz, sp.SampleRate, len(sp.BinsDB))
		}
		// The tone sits at -187.5 kHz: bin (0.5 - 187500/1920000) * 1024 ≈ 412.
		peak, peakDB := 0, -1e9
		for i, v := range sp.BinsDB {
			if v > peakDB {
				peak, peakDB = i, v
			}
		}
		if peak < 410 || peak > 414 {
			t.Errorf("tone peak at bin %d, want ~412", peak)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no spectrum upload")
	}
	for _, r := range sender.commandResults() {
		if r.Status == rxproto.CmdError {
			t.Errorf("unexpected error result: %+v", r)
		}
	}
}

func TestCaptureErrors(t *testing.T) {
	taps := newFakeTaps(t)
	sender := &fakeSender{}
	m := NewManager(Options{Config: rxproto.Defaults(), Sender: sender, Taps: taps, ServerURL: "http://127.0.0.1:1", APIKey: "KEY"})
	cases := map[string]string{
		"unknown channel": `{"capture_id":"a","kind":"audio","channel":"NOPE","seconds":1}`,
		"too long":        `{"capture_id":"a","kind":"audio","channel":"RFS","seconds":31}`,
		"unknown kind":    `{"capture_id":"a","kind":"video"}`,
		"no id":           `{"kind":"spectrum"}`,
	}
	for name, params := range cases {
		m.Handle(rxproto.Command{ID: name, Type: rxproto.CmdCapture, Params: json.RawMessage(params)})
		res := sender.commandResults()
		last := res[len(res)-1]
		if last.ID != name || last.Status != rxproto.CmdError {
			t.Errorf("%s: %+v", name, last)
		}
	}
	// Dongle closed: spectrum fails cleanly.
	taps.mu.Lock()
	taps.open = false
	taps.mu.Unlock()
	m.Handle(rxproto.Command{ID: "closed", Type: rxproto.CmdCapture, Params: json.RawMessage(`{"capture_id":"a","kind":"spectrum"}`)})
	res := sender.commandResults()
	if last := res[len(res)-1]; last.Status != rxproto.CmdError {
		t.Errorf("closed dongle: %+v", last)
	}
}
