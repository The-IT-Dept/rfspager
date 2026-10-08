package main

import (
	"fmt"
	"io"
	"os"

	"github.com/the-it-dept/rfspager/internal/ambe"
	"github.com/the-it-dept/rfspager/internal/p25"
	"github.com/the-it-dept/rfspager/internal/receiver/dsp"
)

// analyze measures reception quality in a wideband capture (rtl_sdr u8 at
// inputRate): ADC clipping, the control channel's decode and NID error rate,
// and for each voice channel the TDMA and vocoder error rates. For comparing
// gain settings, antennas and dongles on the same air.
func analyze(path string, center, cc uint32, voice []uint32) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	type vstat struct {
		hz          uint32
		ch          *p25.Channelizer
		dec         *p25.TDMADecoder
		voc         [2]*ambe.Decoder
		frames, bad int
		bitErrs     int
	}
	ccCh := p25.NewChannelizer(float64(int64(cc)-int64(center)), inputRate)
	ccDec := p25.NewDecoder(func(p25.Message) {})
	var vs []*vstat
	for _, hz := range voice {
		v := &vstat{hz: hz, ch: p25.NewChannelizer(float64(int64(hz)-int64(center)), inputRate)}
		v.voc = [2]*ambe.Decoder{ambe.NewDecoder(), ambe.NewDecoder()}
		v.dec = p25.NewTDMADecoder(0xBEE00, 0x2D1, 0x2D2, func(slot int, d []uint8) {
			_, e := v.voc[slot].DecodeP25P2Dibits(d)
			v.frames++
			if e >= ambe.Uncorrectable {
				v.bad++
			} else {
				v.bitErrs += e
			}
		})
		vs = append(vs, v)
	}

	buf := make([]byte, 1<<18)
	var work []complex128
	var total, clipped int64
	seeded := false
	for {
		n, err := io.ReadFull(f, buf)
		if n == 0 {
			break
		}
		for _, b := range buf[:n] {
			if b == 0 || b == 255 {
				clipped++
			}
		}
		total += int64(n)
		work = dsp.IQToComplex(buf[:n], work)
		ccDec.Process(ccCh.Process(work))
		// Seed the voice AFCs from the control channel once it has settled.
		if !seeded && total > int64(inputRate)*2*2 {
			for _, v := range vs {
				v.dec.SetFreqOffsetHz(ccDec.FreqOffsetHz() * float64(v.hz) / float64(cc))
			}
			seeded = true
		}
		if seeded {
			for _, v := range vs {
				v.dec.Process(v.ch.Process(work))
			}
		}
		if err != nil {
			break
		}
	}

	s := ccDec.Stats()
	fmt.Printf("%s: %.1f s, ADC clipped %.3f%%, CC %d frames, NID %.2f bit errs/frame, TSBK %d/%d ok, AFC %.0f Hz\n",
		path, float64(total)/2/inputRate, 100*float64(clipped)/float64(max(total, 1)),
		s.Frames, float64(s.NIDErrs)/float64(max(s.Frames, 1)), s.BlocksOK, s.Blocks, ccDec.FreqOffsetHz())
	for _, v := range vs {
		t := v.dec.Stats()
		lost := t.BurstTypes[-1]
		if t.Slots == 0 {
			fmt.Printf("  voice %.4f: no traffic\n", float64(v.hz)/1e6)
			continue
		}
		fmt.Printf("  voice %.4f: slots %d, ISCH errs %.1f%%, undecodable bursts %.1f%%, frames %d, uncorrectable %.1f%%, bit errs/frame %.2f\n",
			float64(v.hz)/1e6, t.Slots, pct(t.ISCHErrors, t.Slots), pct(lost, t.Slots), v.frames, pct(v.bad, v.frames),
			float64(v.bitErrs)/float64(max(v.frames-v.bad, 1)))
	}
	return nil
}

func pct(a, b int) float64 { return 100 * float64(a) / float64(max(b, 1)) }
