package receiver

import (
	"math"
	"math/rand"
	"strings"
	"testing"

	"github.com/the-it-dept/rfspager/internal/receiver/dsp"
	"github.com/the-it-dept/rfspager/internal/receiver/pocsag"
)

const (
	testRate    = 1_920_000
	testAudio   = 38_400
	testBufSize = 16384 // complex samples per RTL-SDR buffer, as in cmd/receiver
	testOffset  = 187_500.0
	testMsg     = "21 September 2026 19:24:16 LHPATER7 - 26-128802 - BBQ fire/Bonfire/Yard fire - FIRECALL - 12 SMITH ST,PATERSON,DUNGOG SHIRE (NSW),2421"
)

// fskSource turns a POCSAG bit sequence into RTL-SDR style u8 IQ buffers:
// 2-FSK with ±4.5 kHz deviation on a carrier offsetHz from the dongle
// centre, plus a small frequency error and Gaussian noise. After the last
// bit the carrier drops and only noise remains, as when a transmitter
// unkeys.
type fskSource struct {
	bits       []bool
	baud, fs   float64
	offsetHz   float64
	pos        int
	phase      float64
	r          *rand.Rand
	amp, noise float64
	tailBits   int
}

func newFSK(bits []bool, baud int, offsetHz float64) *fskSource {
	return &fskSource{
		bits: bits, baud: float64(baud), fs: testRate, offsetHz: offsetHz + 600, // 600 Hz tuning error
		r: rand.New(rand.NewSource(7)), amp: 0.5, noise: 0.02, tailBits: 200,
	}
}

func (s *fskSource) totalBuffers() int {
	samples := float64(len(s.bits)+s.tailBits) * s.fs / s.baud
	return int(math.Ceil(samples / testBufSize))
}

func clampU8(v float64) byte {
	return byte(math.Max(0, math.Min(255, math.Round(v))))
}

func (s *fskSource) next() []byte {
	buf := make([]byte, 2*testBufSize)
	for i := 0; i < testBufSize; i++ {
		bitIdx := int(float64(s.pos) * s.baud / s.fs)
		var re, im float64
		if bitIdx < len(s.bits) {
			dev := -4500.0
			if s.bits[bitIdx] {
				dev = 4500
			}
			s.phase += 2 * math.Pi * (s.offsetHz + dev) / s.fs
			if s.phase > math.Pi {
				s.phase -= 2 * math.Pi
			}
			re, im = s.amp*math.Cos(s.phase), s.amp*math.Sin(s.phase)
		}
		re += s.noise * s.r.NormFloat64()
		im += s.noise * s.r.NormFloat64()
		buf[2*i] = clampU8(127.5 + re*127.5)
		buf[2*i+1] = clampU8(127.5 + im*127.5)
		s.pos++
	}
	return buf
}

// collect drains ch.Pages() concurrently; call the returned func to stop and
// get everything decoded so far.
func collect(ch *Channel) func() []pocsag.Page {
	var pages []pocsag.Page
	done := make(chan struct{})
	stop := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case p := <-ch.Pages():
				pages = append(pages, p)
			case <-stop:
				for {
					select {
					case p := <-ch.Pages():
						pages = append(pages, p)
					default:
						return
					}
				}
			}
		}
	}()
	return func() []pocsag.Page {
		close(stop)
		<-done
		return pages
	}
}

// TestChannelDecodesFSKBurst runs a synthesised POCSAG burst through the
// exact IQ → mixer → FIR → FM → slicer → decoder path used in production,
// at every supported baud rate, and expects the message back verbatim.
func TestChannelDecodesFSKBurst(t *testing.T) {
	for _, baud := range SupportedBauds {
		ch := NewChannel("FRNSW", 148_962_500, testOffset, testRate, testAudio)
		stop := collect(ch)
		src := newFSK(pocsag.EncodeTransmission(11416, 0, testMsg), baud, testOffset)
		var work []complex128
		for i := 0; i < src.totalBuffers(); i++ {
			work = dsp.IQToComplex(src.next(), work)
			ch.Process(work)
		}
		pages := stop()
		if len(pages) != 1 {
			t.Errorf("baud=%d: want 1 page, got %d: %+v", baud, len(pages), pages)
			continue
		}
		p := pages[0]
		if p.Address != "0011416" || p.BitRate != baud || p.Alpha != testMsg {
			t.Errorf("baud=%d: decoded %+v", baud, p)
		}
	}
}

// TestChannelGapYieldsCleanPrefix simulates the SDR layer dropping IQ
// buffers mid-burst: the consumer never sees them and calls Gap() before
// processing the next one. The channel must emit the clean prefix of the
// message and nothing else.
func TestChannelGapYieldsCleanPrefix(t *testing.T) {
	for _, dropped := range []int{1, 2, 8} {
		ch := NewChannel("FRNSW", 148_962_500, testOffset, testRate, testAudio)
		stop := collect(ch)
		bits := pocsag.EncodeTransmission(11416, 0, testMsg)
		src := newFSK(bits, 512, testOffset)
		// Land the gap in the second batch: preamble + sync + 17 codewords
		// + second sync + 4 more codewords into the message.
		gapBit := 576 + 32 + 32*17 + 32*4
		gapBuf := int(float64(gapBit) * testRate / 512 / testBufSize)
		var work []complex128
		for i := 0; i < src.totalBuffers(); i++ {
			buf := src.next()
			if i >= gapBuf && i < gapBuf+dropped {
				continue // lost
			}
			if i == gapBuf+dropped {
				ch.Gap()
			}
			work = dsp.IQToComplex(buf, work)
			ch.Process(work)
		}
		pages := stop()
		if len(pages) != 1 {
			t.Errorf("dropped=%d: want exactly 1 page, got %d: %+v", dropped, len(pages), pages)
			continue
		}
		p := pages[0]
		if p.Address != "0011416" || !strings.HasPrefix(testMsg, p.Alpha) || len(p.Alpha) < 40 {
			t.Errorf("dropped=%d: want clean prefix ≥40 chars of message, got %+v", dropped, p)
		}
	}
}

// TestChannelDecodesWeakFSKBurst runs the same burst at a signal level
// where the noise in the 24 kHz channel filter is only ~6 dB below the
// carrier. The matched filter in the slicer recovers the symbols anyway;
// a single-sample slicer at this level either misses or garbles the page.
func TestChannelDecodesWeakFSKBurst(t *testing.T) {
	for _, baud := range SupportedBauds {
		ch := NewChannel("FRNSW", 148_962_500, testOffset, testRate, testAudio)
		stop := collect(ch)
		src := newFSK(pocsag.EncodeTransmission(11416, 0, testMsg), baud, testOffset)
		src.amp, src.noise = 0.12, 0.3
		var work []complex128
		for i := 0; i < src.totalBuffers(); i++ {
			work = dsp.IQToComplex(src.next(), work)
			ch.Process(work)
		}
		pages := stop()
		if len(pages) != 1 || pages[0].Alpha != testMsg || pages[0].Address != "0011416" {
			t.Errorf("baud=%d: want the clean page, got %+v", baud, pages)
		}
	}
}
