// Package receiver wires together SDR, DSP, and POCSAG decoding for one
// frequency channel that has been tuned by a shared RTL-SDR dongle.
package receiver

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/the-it-dept/rfspager/internal/receiver/dsp"
	"github.com/the-it-dept/rfspager/internal/receiver/pocsag"
)

// PageSender is the transport contract the Forwarder uses. Implemented by
// the authenticated wsclient.Client and in-memory test senders.
type PageSender interface {
	SendPage(addr, msg string, when time.Time, fn, baud int)
}

// SupportedBauds are the POCSAG bit rates we attempt to decode in parallel
// on every channel. A real burst will only lock at one of these; the other
// slicers harmlessly produce noise that fails sync detection.
var SupportedBauds = []int{512, 1200, 2400}

// Channel is one tuned POCSAG frequency: takes IQ samples relative to the
// dongle's centre frequency, shifts them down to DC, decimates, FM-
// demodulates, and runs one bit-slicer + decoder per supported baud rate.
type Channel struct {
	Label    string
	FreqHz   uint32
	OffsetHz float64 // FreqHz - dongle centre

	mixer *dsp.Mixer
	fir   *dsp.FIRDecimator
	fm    dsp.FMDemod
	legs  []bandLeg

	mixed []complex128 // scratch: block after the mixer
	audio []complex128 // scratch: block after FIR + decimation

	out chan pocsag.Page
}

// bandLeg pairs a bit slicer with a POCSAG decoder for one baud rate.
type bandLeg struct {
	slicer  *dsp.BitSlicer
	decoder *pocsag.Decoder
}

// NewChannel constructs a Channel tuned `offsetHz` away from the dongle's
// centre frequency. inputRate is the raw IQ rate; audioRate is the rate
// after FIR decimation.
func NewChannel(label string, freqHz uint32, offsetHz float64, inputRate, audioRate uint32) *Channel {
	decim := int(inputRate / audioRate)
	// 12 kHz cutoff: POCSAG with ±4.5 kHz deviation at 2400 baud occupies
	// roughly 14–16 kHz total, so we want passband to comfortably contain
	// that. Only decimated outputs are computed, so 97 taps at 38.4 kHz is
	// ~3.7 MMAC/s per channel — cheap.
	taps := dsp.LowpassTaps(12_000, float64(inputRate), 96)
	pages := make(chan pocsag.Page, 16)

	ch := &Channel{
		Label:    label,
		FreqHz:   freqHz,
		OffsetHz: offsetHz,
		mixer:    dsp.NewMixer(offsetHz, float64(inputRate)),
		fir:      dsp.NewFIRDecimator(taps, decim),
		out:      pages,
	}
	for _, baud := range SupportedBauds {
		ch.legs = append(ch.legs, bandLeg{
			slicer:  dsp.NewBitSlicer(float64(audioRate), float64(baud)),
			decoder: pocsag.New(baud, pages),
		})
	}
	return ch
}

// Pages returns the channel that emits decoded pages.
func (c *Channel) Pages() <-chan pocsag.Page { return c.out }

// Process consumes one buffer of complex IQ samples and runs them through
// the per-channel pipeline. Pages are emitted on c.Pages().
//
// The full-rate stages run block-at-a-time; only the ~38.4 kHz audio-rate
// stages (FM demod, slicers, decoders) run per sample.
func (c *Channel) Process(samples []complex128) {
	c.mixed = c.mixer.MixBlock(samples, c.mixed)
	c.audio = c.fir.ProcessBlock(c.mixed, c.audio)
	for _, a := range c.audio {
		demod := c.fm.Demod(a)
		for i := range c.legs {
			bit, gotBit := c.legs[i].slicer.Push(demod)
			if gotBit {
				c.legs[i].decoder.Feed(bit)
			}
		}
	}
}

// Gap tells the channel the IQ stream had a discontinuity (the SDR layer
// dropped buffers), so an unknown number of bits went missing. Every
// decoder finalises whatever it had and re-hunts for sync; the slicers and
// filters need no reset — they re-lock within a few symbols.
func (c *Channel) Gap() {
	for i := range c.legs {
		c.legs[i].decoder.Resync()
	}
}

// Forwarder pumps pages from a Channel onto whatever upstream transport
// the receiver was configured with.
type Forwarder struct {
	Sender PageSender
	// PostAll bypasses shouldPost. Every decoded page gets sent up
	// (with the message we'd otherwise log). Used for pipeline tests.
	PostAll bool
}

// Run consumes pages from ch.Pages() until ctx is cancelled. Substantive
// pages are sent via WS; noise-looking decodes are logged only.
func (f *Forwarder) Run(ctx context.Context, ch *Channel) {
	for {
		select {
		case <-ctx.Done():
			return
		case page := <-ch.Pages():
			// For logging show whichever interpretation has content (so we
			// can still see numeric pages going past). For posting we *only*
			// want alpha messages — we ignore function bits because noise
			// flips them even on real alpha bursts, but a real alpha page
			// always lands as a substantive printable string in page.Alpha.
			logMsg := page.Alpha
			if logMsg == "" {
				logMsg = page.Numeric
			}
			post := f.PostAll || shouldPost(page.Alpha)
			slog.Info("page",
				"channel", ch.Label,
				"freq_mhz", float64(ch.FreqHz)/1e6,
				"baud", page.BitRate,
				"address", page.Address,
				"fn", page.Function,
				"posted", post,
				"msg", logMsg,
			)
			if !post {
				continue
			}
			// In PostAll mode, fall back to numeric when alpha is empty so
			// purely-numeric beacons (e.g. clock-tick 0180111) make it up
			// the wire for end-to-end testing.
			sendMsg := page.Alpha
			if sendMsg == "" {
				sendMsg = page.Numeric
			}
			f.Sender.SendPage(page.Address, sendMsg, time.Now(), page.Function, page.BitRate)
		}
	}
}

// shouldPost decides whether to forward a decoded page. Alpha-only: a
// non-empty alpha string with enough printable characters to look like a
// real dispatch message. Function bits are ignored because they get
// corrupted by noise even when the address + message survive.
//
// Gates (any single one disqualifies):
//   - length ≥ 6 — short numeric/tone artefacts dropped
//   - length > 12 must contain at least one ASCII space — real dispatch
//     text always has word boundaries; noise companions that survive
//     BCH false-correction are dense streams of mixed glyphs without
//     whitespace ("WHC+darp\n98E\"", "nlLXZF]EMeIu^\".WHC+darp\n9", etc).
//   - ≥ 85% of characters in the dispatch-printable class — real messages
//     sit around 0.92, false-positive noise around 0.70–0.78.
func shouldPost(alpha string) bool {
	msg := strings.TrimSpace(alpha)
	if len(msg) < 6 {
		return false
	}
	if len(msg) > 12 && !strings.ContainsRune(msg, ' ') {
		return false
	}
	good := 0
	for _, r := range msg {
		switch {
		case r >= 'A' && r <= 'Z',
			r >= 'a' && r <= 'z',
			r >= '0' && r <= '9',
			r == ' ', r == ':', r == '-', r == '.', r == ',', r == '/', r == '#', r == '(', r == ')', r == '*':
			good++
		}
	}
	return float64(good)/float64(len(msg)) >= 0.85
}
