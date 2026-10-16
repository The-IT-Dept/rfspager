// Package receiver wires together SDR, DSP, and POCSAG decoding for one
// frequency channel that has been tuned by a shared RTL-SDR dongle.
package receiver

import (
	"context"
	"log/slog"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/the-it-dept/rfspager/internal/receiver/dsp"
	"github.com/the-it-dept/rfspager/internal/receiver/pocsag"
	"github.com/the-it-dept/rfspager/internal/rxproto"
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

// Decoders is the audio-rate tail of a channel: one bit slicer plus one
// POCSAG decoder per supported baud, all fed the same FM-demodulated
// samples. Channel uses it live; cmd/replay uses it on recorded captures so
// both run exactly the same bit path.
type Decoders struct {
	legs []bandLeg
}

// bandLeg is one baud rate's receive chain after the channel filter: an
// IF filter no wider than that baud needs, its own FM discriminator, a
// bit slicer and a POCSAG decoder.
//
// The channel FIR is 12 kHz wide so 2400 baud fits. A 512-baud signal
// with ±4.5 kHz deviation occupies under 10 kHz, so for it that extra
// bandwidth is pure noise at the discriminator, and FM demodulation
// falls off a cliff once the carrier-to-noise ratio in the IF bandwidth
// drops below about 10 dB. Halving the IF bandwidth for the slow bauds
// buys ~3 dB at exactly that cliff, which is where distant pages live.
type bandLeg struct {
	iff     *dsp.FIRDecimator // nil: use the channel-wide audio as is
	filt    []complex128      // scratch: this leg's IF-filtered block
	fm      dsp.FMDemod
	slicer  *dsp.BitSlicer
	decoder *pocsag.Decoder
}

// ifCutoffHz is the per-baud IF lowpass cutoff at the audio rate, or 0
// for no extra filtering. Deviation ±4.5 kHz plus half the baud plus
// room for ~1.5 kHz of tuner and transmitter frequency error.
func ifCutoffHz(baud int) float64 {
	switch baud {
	case 512:
		return 6_500
	case 1200:
		return 7_500
	}
	return 0
}

// NewDecoders builds the slicer + decoder pairs for audioRate; pages from
// every baud are emitted on out.
func NewDecoders(audioRate uint32, out chan pocsag.Page) *Decoders {
	d := &Decoders{}
	for _, baud := range SupportedBauds {
		leg := bandLeg{
			slicer:  dsp.NewBitSlicer(float64(audioRate), float64(baud)),
			decoder: pocsag.New(baud, out),
		}
		if cutoff := ifCutoffHz(baud); cutoff > 0 {
			// 65 taps at 38.4 kHz: ~2 kHz transition, 53 dB stopband, and
			// about 10 MMAC/s per leg, which is nothing next to the mixer.
			leg.iff = dsp.NewFIRDecimator(dsp.LowpassTaps(cutoff, float64(audioRate), 64), 1)
		}
		d.legs = append(d.legs, leg)
	}
	return d
}

// FeedIQ pushes one block of channel-filtered complex baseband through
// every leg: each applies its own IF filter and discriminator before
// slicing. This is the production path; Feed is for pre-demodulated
// audio where no IF filtering is possible.
func (d *Decoders) FeedIQ(audio []complex128) {
	for i := range d.legs {
		leg := &d.legs[i]
		src := audio
		if leg.iff != nil {
			leg.filt = leg.iff.ProcessBlock(audio, leg.filt)
			src = leg.filt
		}
		for _, a := range src {
			bit, conf, gotBit := leg.slicer.PushSoft(leg.fm.Demod(a))
			if gotBit {
				leg.decoder.FeedSoft(bit, conf)
			}
		}
	}
}

// Feed pushes one demodulated sample (radians/sample) through every leg.
func (d *Decoders) Feed(demod float64) {
	for i := range d.legs {
		bit, gotBit := d.legs[i].slicer.Push(demod)
		if gotBit {
			d.legs[i].decoder.Feed(bit)
		}
	}
}

// Resync tells every decoder the bit stream has a discontinuity.
func (d *Decoders) Resync() {
	for i := range d.legs {
		d.legs[i].decoder.Resync()
	}
}

// Stats sums the cumulative decoder counters over all bauds.
func (d *Decoders) Stats() pocsag.Stats {
	var s pocsag.Stats
	for i := range d.legs {
		s = s.Add(d.legs[i].decoder.Stats())
	}
	return s
}

// Power histogram: one bin per dB from histMinDB up to 0 dBFS, where 1.0 is
// full scale on the complex baseband after the channel filter.
const (
	histMinDB = -120
	histBins  = 121
	// carrierMarginDB is how far above the noise floor a block must sit to
	// count as carrier present.
	carrierMarginDB = 6
)

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
	dec   *Decoders

	mixed []complex128 // scratch: block after the mixer
	audio []complex128 // scratch: block after FIR + decimation
	demod []float64    // scratch: FM demod of audio

	out chan pocsag.Page

	// tap is the capture in progress, if any. Loaded once per block.
	tap atomic.Pointer[Capture]

	histMu    sync.Mutex
	hist      [histBins]uint32
	blocks    uint32
	prevStats pocsag.Stats // decoder counters at the previous Snapshot
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

	return &Channel{
		Label:    label,
		FreqHz:   freqHz,
		OffsetHz: offsetHz,
		mixer:    dsp.NewMixer(offsetHz, float64(inputRate)),
		fir:      dsp.NewFIRDecimator(taps, decim),
		dec:      NewDecoders(audioRate, pages),
		out:      pages,
	}
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
	c.recordPower(c.audio)

	c.demod = c.demod[:0]
	for _, a := range c.audio {
		c.demod = append(c.demod, c.fm.Demod(a))
	}
	if tp := c.tap.Load(); tp != nil {
		if tp.feed(c.audio, c.demod) {
			c.tap.CompareAndSwap(tp, nil)
			close(tp.done)
		}
	}
	c.dec.FeedIQ(c.audio)
}

// Gap tells the channel the IQ stream had a discontinuity (the SDR layer
// dropped buffers), so an unknown number of bits went missing. Every
// decoder finalises whatever it had and re-hunts for sync; the slicers and
// filters need no reset — they re-lock within a few symbols.
func (c *Channel) Gap() { c.dec.Resync() }

// recordPower bins the block's mean power (dBFS) into the histogram.
func (c *Channel) recordPower(block []complex128) {
	if len(block) == 0 {
		return
	}
	var p float64
	for _, a := range block {
		p += real(a)*real(a) + imag(a)*imag(a)
	}
	p /= float64(len(block))
	db := float64(histMinDB)
	if p > 0 {
		db = 10 * math.Log10(p)
	}
	idx := int(math.Floor(db)) - histMinDB
	if idx < 0 {
		idx = 0
	} else if idx >= histBins {
		idx = histBins - 1
	}
	c.histMu.Lock()
	c.hist[idx]++
	c.blocks++
	c.histMu.Unlock()
}

// Snapshot reports the channel's health for the interval since the previous
// call and resets the interval state (histogram, decoder deltas). Call it
// from one goroutine only.
func (c *Channel) Snapshot() rxproto.ChannelStats {
	c.histMu.Lock()
	hist, n := c.hist, c.blocks
	c.hist = [histBins]uint32{}
	c.blocks = 0
	c.histMu.Unlock()

	floorBin := percentileBin(hist, n, 0.10)
	powerBin := percentileBin(hist, n, 0.90)
	var above uint32
	for b := floorBin + carrierMarginDB + 1; b < histBins && b >= 0; b++ {
		above += hist[b]
	}
	st := rxproto.ChannelStats{
		Label:          c.Label,
		FreqHz:         c.FreqHz,
		PowerDBFS:      float64(powerBin + histMinDB),
		NoiseFloorDBFS: float64(floorBin + histMinDB),
		SNRDB:          float64(powerBin - floorBin),
		LastPageAgeS:   -1,
	}
	if n > 0 {
		st.CarrierPct = 100 * float64(above) / float64(n)
	}

	cur := c.dec.Stats()
	d := cur.Sub(c.prevStats)
	c.prevStats = cur
	st.SyncLocks = int(d.SyncLocks)
	st.SyncLosses = int(d.SyncLosses)
	st.CWClean = int(d.CWClean)
	st.CWCorrected = int(d.CWCorrected)
	st.CWUncorrectable = int(d.CWUncorrectable)
	st.CWChased = int(d.CWChased)
	st.Pages = int(d.Pages)
	st.PagesTruncated = int(d.PagesTruncated)
	if !cur.LastPageAt.IsZero() {
		st.LastPageAgeS = int(time.Since(cur.LastPageAt).Seconds())
	}
	return st
}

// percentileBin returns the lowest bin whose cumulative count reaches
// q of n blocks; bin 0 when there are no blocks.
func percentileBin(hist [histBins]uint32, n uint32, q float64) int {
	if n == 0 {
		return 0
	}
	target := max(uint32(math.Ceil(q*float64(n))), 1)
	var cum uint32
	for b := range hist {
		cum += hist[b]
		if cum >= target {
			return b
		}
	}
	return histBins - 1
}

// Capture is an in-progress recording of one channel's signal, filled by
// Process and complete when Done is closed.
//
// Formats, both little-endian int16:
//
//   - audio: the FM demodulator output, one sample per audio-rate sample,
//     scaled by 32767/pi so ±pi rad/sample is full scale.
//   - iq: the complex baseband after the channel FIR, interleaved I, Q.
//     Scaled by a fixed 32767 (1.0 = full scale, the same reference as the
//     dBFS numbers in stats) rather than normalised to the signal, so a
//     capture is comparable across receivers and does not clip when a
//     burst starts after a quiet first block. The 8-bit dongle's own
//     quantisation noise sits far above int16 resolution, so nothing is
//     lost on weak signals.
type Capture struct {
	Kind      string
	remaining int // samples still wanted
	buf       []byte
	done      chan struct{}
}

// StartCapture begins recording kind (rxproto.CaptureAudio or
// rxproto.CaptureIQ) for the given number of audio-rate samples. One
// capture runs per channel at a time.
func (c *Channel) StartCapture(kind string, samples int) (*Capture, error) {
	if kind != rxproto.CaptureAudio && kind != rxproto.CaptureIQ {
		return nil, errUnknownCaptureKind
	}
	if samples <= 0 {
		return nil, errCaptureLength
	}
	per := 2
	if kind == rxproto.CaptureIQ {
		per = 4
	}
	cp := &Capture{Kind: kind, remaining: samples, buf: make([]byte, 0, samples*per), done: make(chan struct{})}
	if !c.tap.CompareAndSwap(nil, cp) {
		return nil, errCaptureBusy
	}
	return cp, nil
}

// CancelCapture detaches a capture that will not be waited for. Done is
// never closed for a cancelled capture.
func (c *Channel) CancelCapture(cp *Capture) { c.tap.CompareAndSwap(cp, nil) }

// Done is closed once the requested number of samples has been recorded.
func (cp *Capture) Done() <-chan struct{} { return cp.done }

// Bytes returns the recording. Only valid after Done is closed.
func (cp *Capture) Bytes() []byte { return cp.buf }

// feed appends from the block and reports whether the capture is complete.
func (cp *Capture) feed(audio []complex128, demod []float64) bool {
	switch cp.Kind {
	case rxproto.CaptureAudio:
		for _, d := range demod {
			if cp.remaining == 0 {
				break
			}
			cp.put(int16Clamp(d * 32767 / math.Pi))
			cp.remaining--
		}
	case rxproto.CaptureIQ:
		for _, a := range audio {
			if cp.remaining == 0 {
				break
			}
			cp.put(int16Clamp(real(a) * 32767))
			cp.put(int16Clamp(imag(a) * 32767))
			cp.remaining--
		}
	}
	return cp.remaining == 0
}

func (cp *Capture) put(v int16) {
	cp.buf = append(cp.buf, byte(uint16(v)), byte(uint16(v)>>8))
}

func int16Clamp(v float64) int16 {
	switch {
	case v > 32767:
		return 32767
	case v < -32768:
		return -32768
	}
	return int16(math.Round(v))
}

type captureError string

func (e captureError) Error() string { return string(e) }

const (
	errUnknownCaptureKind = captureError("unknown capture kind")
	errCaptureLength      = captureError("capture length must be positive")
	errCaptureBusy        = captureError("a capture is already running on this channel")
)

// QualitySender is implemented by transports that can carry the page's
// decode quality alongside the text; others get SendPage.
type QualitySender interface {
	SendPageQ(addr, msg string, when time.Time, fn, baud int, q *rxproto.PageQuality)
}

// pageQuality converts the decoder's assessment to its wire form.
func pageQuality(q pocsag.Quality) *rxproto.PageQuality {
	out := &rxproto.PageQuality{
		Codewords:     q.Codewords,
		Corrected:     q.Corrected,
		Uncorrectable: q.Uncorrectable,
		Truncated:     q.Truncated,
	}
	if len(q.DamagedRanges) > 0 {
		out.Damaged = append([][2]int(nil), q.DamagedRanges...)
	}
	return out
}

// DecodedPage is one decode as the forwarder saw it, for local observers
// (the receiver's own UI and the host's webhooks).
type DecodedPage struct {
	Channel  string
	FreqHz   uint32
	Time     time.Time
	Address  string
	Function int
	Baud     int
	Message  string // alpha, or numeric when alpha is empty
	Posted   bool   // passed the content filter (or post_all) and went upstream
	Quality  *rxproto.PageQuality
}

// Forwarder pumps pages from a Channel onto whatever upstream transport
// the receiver was configured with.
type Forwarder struct {
	Sender PageSender
	// Observe, when set, sees every decode (posted or filtered) before it
	// is sent. It must not block: it runs on the channel's page loop.
	Observe func(DecodedPage)
	// postAll bypasses shouldPost. Every decoded page gets sent up
	// (with the message we'd otherwise log). Used for pipeline tests.
	postAll atomic.Bool
}

// NewForwarder builds a Forwarder; postAll bypasses the content filter.
func NewForwarder(sender PageSender, postAll bool) *Forwarder {
	f := &Forwarder{Sender: sender}
	f.postAll.Store(postAll)
	return f
}

// SetPostAll switches the content filter bypass at runtime.
func (f *Forwarder) SetPostAll(v bool) { f.postAll.Store(v) }

// PostAll reports whether the content filter is bypassed.
func (f *Forwarder) PostAll() bool { return f.postAll.Load() }

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
			post := f.postAll.Load() || shouldPost(page.Alpha)
			slog.Info("page",
				"channel", ch.Label,
				"freq_mhz", float64(ch.FreqHz)/1e6,
				"baud", page.BitRate,
				"address", page.Address,
				"fn", page.Function,
				"posted", post,
				"msg", logMsg,
				"corrected", page.Quality.Corrected,
				"uncorrectable", page.Quality.Uncorrectable,
				"truncated", page.Quality.Truncated,
			)
			now := time.Now()
			if post {
				// In PostAll mode, fall back to numeric when alpha is empty so
				// purely-numeric beacons (e.g. clock-tick 0180111) make it up
				// the wire for end-to-end testing. (logMsg is exactly that.)
				if qs, ok := f.Sender.(QualitySender); ok {
					qs.SendPageQ(page.Address, logMsg, now, page.Function, page.BitRate, pageQuality(page.Quality))
				} else {
					f.Sender.SendPage(page.Address, logMsg, now, page.Function, page.BitRate)
				}
			}
			// After the upstream send, so local observers never delay it.
			if f.Observe != nil {
				f.Observe(DecodedPage{
					Channel: ch.Label, FreqHz: ch.FreqHz, Time: now,
					Address: page.Address, Function: page.Function, Baud: page.BitRate,
					Message: logMsg, Posted: post, Quality: pageQuality(page.Quality),
				})
			}
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
