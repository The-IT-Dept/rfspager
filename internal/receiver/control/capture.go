package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/the-it-dept/rfspager/internal/receiver"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// Captures.
//
// spectrum: one averaged 1024-bin power spectrum of the raw dongle IQ over
// about one second, uploaded as JSON {centre_hz, sample_rate, bins_db}
// with bins running from centre - fs/2 to centre + fs/2.
//
// audio / iq: N seconds of one channel, recorded by the channel's capture
// tap (see receiver.Capture for the int16 formats) and uploaded as
// application/octet-stream with X-Capture-Rate = rxproto.AudioRate.
//
// Every capture goes to PUT <primary server>/api/receiver/captures/<id>
// with the apikey header plus X-Capture-Kind, X-Capture-Rate and
// X-Capture-Channel.

// SpectrumBins is the FFT size for spectrum captures.
const SpectrumBins = 1024

type spectrumJSON struct {
	CentreHz   uint32    `json:"centre_hz"`
	SampleRate uint32    `json:"sample_rate"`
	BinsDB     []float64 `json:"bins_db"`
}

func (m *Manager) capture(cmd rxproto.Command) error {
	var p rxproto.CaptureParams
	if err := json.Unmarshal(cmd.Params, &p); err != nil {
		return fmt.Errorf("bad capture params: %w", err)
	}
	if p.CaptureID == "" {
		return errors.New("capture needs a capture_id")
	}
	if m.opts.Taps == nil {
		return errors.New("captures are not available on this receiver")
	}
	progress := func(stage, msg string) { m.report(cmd, rxproto.CmdProgress, stage, msg) }

	var (
		body        []byte
		contentType string
		rate        uint32
		channel     string
	)
	switch p.Kind {
	case rxproto.CaptureSpectrum:
		cfg := m.Config()
		centre, _ := cfg.CentreHz()
		sp := receiver.NewSpectrum(SpectrumBins, int(cfg.SampleRate))
		if err := m.opts.Taps.Spectrum(sp); err != nil {
			return err
		}
		progress("recording", "averaging 1 s of IQ")
		select {
		case <-sp.Done():
		case <-time.After(15 * time.Second):
			m.opts.Taps.CancelSpectrum(sp)
			return errors.New("spectrum timed out: the dongle is not delivering samples")
		}
		raw, err := json.Marshal(spectrumJSON{CentreHz: centre, SampleRate: cfg.SampleRate, BinsDB: sp.BinsDB()})
		if err != nil {
			return err
		}
		body, contentType, rate = raw, "application/json", cfg.SampleRate
	case rxproto.CaptureAudio, rxproto.CaptureIQ:
		seconds := p.Seconds
		if seconds <= 0 {
			seconds = 10
		}
		if seconds > rxproto.MaxCaptureSeconds {
			return fmt.Errorf("seconds must be at most %d", rxproto.MaxCaptureSeconds)
		}
		ch := m.opts.Taps.Channel(p.Channel)
		if ch == nil {
			return fmt.Errorf("unknown channel %q", p.Channel)
		}
		cp, err := ch.StartCapture(p.Kind, seconds*int(rxproto.AudioRate))
		if err != nil {
			return err
		}
		progress("recording", fmt.Sprintf("%d s of %s on %s", seconds, p.Kind, ch.Label))
		select {
		case <-cp.Done():
		case <-time.After(time.Duration(seconds)*time.Second + 15*time.Second):
			ch.CancelCapture(cp)
			return errors.New("capture timed out: the channel stopped receiving samples")
		}
		body, contentType, rate, channel = cp.Bytes(), "application/octet-stream", rxproto.AudioRate, ch.Label
	default:
		return fmt.Errorf("unknown capture kind %q", p.Kind)
	}

	progress("uploading", fmt.Sprintf("%d bytes", len(body)))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := m.upload(ctx, p.CaptureID, p.Kind, channel, rate, contentType, body); err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	m.report(cmd, rxproto.CmdOK, "", fmt.Sprintf("%s capture uploaded (%d bytes)", p.Kind, len(body)))
	return nil
}

func (m *Manager) upload(ctx context.Context, captureID, kind, channel string, rate uint32, contentType string, body []byte) error {
	u := strings.TrimRight(m.opts.ServerURL, "/") + "/api/receiver/captures/" + captureID
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.ContentLength = int64(len(body))
	req.Header.Set("apikey", m.opts.APIKey)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Capture-Kind", kind)
	req.Header.Set("X-Capture-Rate", strconv.FormatUint(uint64(rate), 10))
	if channel != "" {
		req.Header.Set("X-Capture-Channel", channel)
	}
	resp, err := m.opts.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("server returned %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}
