package receiver

import (
	"encoding/binary"
	"math"
	"math/cmplx"
	"testing"

	"github.com/the-it-dept/rfspager/internal/receiver/dsp"
	"github.com/the-it-dept/rfspager/internal/receiver/pocsag"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// feedBlocks runs synthetic post-FIR blocks of a given power straight into
// the histogram, bypassing the mixer and FIR.
func feedBlocks(ch *Channel, dbfs float64, n int) {
	amp := math.Pow(10, dbfs/20)
	block := make([]complex128, 64)
	for i := range block {
		block[i] = complex(amp, 0)
	}
	for range n {
		ch.recordPower(block)
	}
}

func TestSnapshotPercentilesFromHistogram(t *testing.T) {
	ch := NewChannel("RFS", 148_587_500, -187_500, testRate, testAudio)
	// 80 noise-floor blocks at -70 dBFS, 20 carrier blocks at -30 dBFS.
	feedBlocks(ch, -70, 80)
	feedBlocks(ch, -30, 20)
	st := ch.Snapshot()
	if st.NoiseFloorDBFS != -70 {
		t.Errorf("floor %v, want -70", st.NoiseFloorDBFS)
	}
	if st.PowerDBFS != -30 {
		t.Errorf("power %v, want -30", st.PowerDBFS)
	}
	if st.SNRDB != 40 {
		t.Errorf("snr %v, want 40", st.SNRDB)
	}
	if st.CarrierPct != 20 {
		t.Errorf("carrier %v%%, want 20", st.CarrierPct)
	}
	if st.LastPageAgeS != -1 {
		t.Errorf("last page age %d, want -1 with no pages", st.LastPageAgeS)
	}
	if st.Label != "RFS" || st.FreqHz != 148_587_500 {
		t.Errorf("identity: %+v", st)
	}

	// The interval resets: a fresh snapshot sees nothing.
	st = ch.Snapshot()
	if st.CarrierPct != 0 || st.PowerDBFS != -120 || st.NoiseFloorDBFS != -120 {
		t.Errorf("histogram not reset: %+v", st)
	}
}

func TestSnapshotClampsOutOfRangePower(t *testing.T) {
	ch := NewChannel("RFS", 148_587_500, -187_500, testRate, testAudio)
	feedBlocks(ch, 20, 5)                 // louder than full scale
	feedBlocks(ch, -200, 5)               // quieter than the lowest bin
	ch.recordPower(nil)                   // empty block ignored
	ch.recordPower(make([]complex128, 8)) // exact zero power
	st := ch.Snapshot()
	if st.PowerDBFS != 0 || st.NoiseFloorDBFS != -120 {
		t.Errorf("clamping: %+v", st)
	}
}

func TestSnapshotReportsDecoderDeltas(t *testing.T) {
	ch := NewChannel("FRNSW", 148_962_500, testOffset, testRate, testAudio)
	stop := collect(ch)
	src := newFSK(pocsag.EncodeTransmission(11416, 0, testMsg), 1200, testOffset)
	var work []complex128
	for i := 0; i < src.totalBuffers(); i++ {
		work = dsp.IQToComplex(src.next(), work)
		ch.Process(work)
	}
	if pages := stop(); len(pages) != 1 {
		t.Fatalf("want 1 page, got %d", len(pages))
	}
	st := ch.Snapshot()
	if st.Pages != 1 || st.SyncLocks < 1 || st.CWClean == 0 {
		t.Errorf("decoder deltas: %+v", st)
	}
	if st.LastPageAgeS < 0 {
		t.Errorf("last page age %d, want >= 0", st.LastPageAgeS)
	}
	// The carrier is keyed for nearly the whole run, so p10 and p90 both
	// sit at the 0.5 amplitude carrier level (about -6 dBFS).
	if st.PowerDBFS < -12 || st.PowerDBFS > 0 {
		t.Errorf("power %v dBFS, want about -6", st.PowerDBFS)
	}
	// Deltas, not totals: nothing new since.
	st = ch.Snapshot()
	if st.Pages != 0 || st.CWClean != 0 {
		t.Errorf("second snapshot should be a zero delta: %+v", st)
	}
}

func TestCaptureAudioAndIQ(t *testing.T) {
	ch := NewChannel("FRNSW", 148_962_500, testOffset, testRate, testAudio)
	go func() {
		for range ch.Pages() {
		}
	}()
	const want = 1000
	if _, err := ch.StartCapture("video", want); err == nil {
		t.Fatal("unknown kind accepted")
	}
	audio, err := ch.StartCapture(rxproto.CaptureAudio, want)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ch.StartCapture(rxproto.CaptureIQ, want); err == nil {
		t.Fatal("second concurrent capture accepted")
	}
	src := newFSK(pocsag.EncodeTransmission(11416, 0, "HELLO"), 512, testOffset)
	var work []complex128
	for i := 0; i < src.totalBuffers(); i++ {
		work = dsp.IQToComplex(src.next(), work)
		ch.Process(work)
	}
	select {
	case <-audio.Done():
	default:
		t.Fatal("audio capture did not complete")
	}
	if got := len(audio.Bytes()); got != want*2 {
		t.Fatalf("audio bytes %d, want %d", got, want*2)
	}
	// The preamble is ±4.5 kHz FSK: demod samples sit near ±2π·4500/38400
	// rad/sample, i.e. about ±0.74·32767/π ≈ ±7700 after scaling.
	var big int
	for i := 0; i < want; i++ {
		v := int16(binary.LittleEndian.Uint16(audio.Bytes()[2*i:]))
		if v > 5000 || v < -5000 {
			big++
		}
	}
	if big < want/2 {
		t.Errorf("only %d of %d audio samples look like FSK deviation", big, want)
	}

	iq, err := ch.StartCapture(rxproto.CaptureIQ, want)
	if err != nil {
		t.Fatal(err)
	}
	src = newFSK(pocsag.EncodeTransmission(11416, 0, "HELLO"), 512, testOffset)
	for i := 0; i < src.totalBuffers(); i++ {
		work = dsp.IQToComplex(src.next(), work)
		ch.Process(work)
	}
	<-iq.Done()
	if got := len(iq.Bytes()); got != want*4 {
		t.Fatalf("iq bytes %d, want %d", got, want*4)
	}
	// The source is a 0.5 amplitude carrier: magnitudes should sit near
	// 0.5 · 32767 once the FIR has settled.
	var sum float64
	for i := 100; i < want; i++ {
		re := float64(int16(binary.LittleEndian.Uint16(iq.Bytes()[4*i:])))
		im := float64(int16(binary.LittleEndian.Uint16(iq.Bytes()[4*i+2:])))
		sum += cmplx.Abs(complex(re, im))
	}
	if mean := sum / float64(want-100); mean < 0.35*32767 || mean > 0.65*32767 {
		t.Errorf("iq magnitude %.0f, want about %.0f", mean, 0.5*32767)
	}

	// A cancelled capture never completes and frees the slot.
	c3, _ := ch.StartCapture(rxproto.CaptureAudio, want)
	ch.CancelCapture(c3)
	if _, err := ch.StartCapture(rxproto.CaptureAudio, 1); err != nil {
		t.Errorf("slot not freed after cancel: %v", err)
	}
}

func TestSpectrumFindsTone(t *testing.T) {
	const bins = 1024
	const fs = 1_920_000.0
	const toneHz = 187_500.0
	sp := NewSpectrum(bins, 8*bins)
	block := make([]complex128, 4*bins)
	var phase float64
	fill := func() {
		for i := range block {
			block[i] = complex(0.5*math.Cos(phase), 0.5*math.Sin(phase))
			phase += 2 * math.Pi * toneHz / fs
		}
	}
	fill()
	if sp.Feed(block) {
		t.Fatal("complete too early")
	}
	fill()
	if !sp.Feed(block) {
		t.Fatal("not complete after enough samples")
	}
	select {
	case <-sp.Done():
	default:
		t.Fatal("Done not closed")
	}
	out := sp.BinsDB()
	if len(out) != bins {
		t.Fatalf("got %d bins", len(out))
	}
	peak, peakDB := 0, -1e9
	for i, v := range out {
		if v > peakDB {
			peak, peakDB = i, v
		}
	}
	// fftshifted: bin 0 is -fs/2, so the tone lands at (toneHz/fs + 0.5) * bins.
	wantBin := int(math.Round((toneHz/fs + 0.5) * bins))
	if peak < wantBin-1 || peak > wantBin+1 {
		t.Errorf("peak at bin %d, want %d", peak, wantBin)
	}
	// A 0.5 amplitude tone reads -6 dB with coherent-gain normalisation.
	if peakDB < -8 || peakDB > -4 {
		t.Errorf("peak %.1f dB, want about -6", peakDB)
	}
	if far := out[(peak+bins/2)%bins]; far > peakDB-60 {
		t.Errorf("far bin %.1f dB too close to the peak", far)
	}
}

func TestForwarderPostAllToggle(t *testing.T) {
	f := NewForwarder(nil, false)
	if f.PostAll() {
		t.Fatal("default should be false")
	}
	f.SetPostAll(true)
	if !f.PostAll() {
		t.Fatal("toggle lost")
	}
}
