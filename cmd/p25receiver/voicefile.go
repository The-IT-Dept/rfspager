package main

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/the-it-dept/rfspager/internal/ambe"
	"github.com/the-it-dept/rfspager/internal/p25"
)

func init() {
	newVocoder = func() vocoder { return ambe.NewDecoder() }
}

// decodeVoiceFile decodes a -record capture (48 kS/s cf32 of one Phase 2
// voice channel) into one WAV per logical slot, for checking the voice
// chain offline. afcHz seeds the AFC (the control channel's estimate).
func decodeVoiceFile(path, outDir string, wacn uint32, sysid, nac uint16, afcHz float64) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	iq := make([]complex128, len(raw)/8)
	for i := range iq {
		re := math.Float32frombits(binary.LittleEndian.Uint32(raw[8*i:]))
		im := math.Float32frombits(binary.LittleEndian.Uint32(raw[8*i+4:]))
		iq[i] = complex(float64(re), float64(im))
	}
	var (
		voc    [2]*ambe.Decoder
		pcm    [2][]int16
		frames [2]int
		errs   [2]int
		bad    [2]int
	)
	for s := range voc {
		voc[s] = ambe.NewDecoder()
	}
	dec := p25.NewTDMADecoder(wacn, sysid, nac, func(slot int, d []uint8) {
		out, e := voc[slot].DecodeP25P2Dibits(d)
		frames[slot]++
		if e == ambe.Uncorrectable {
			bad[slot]++
		} else {
			errs[slot] += e
		}
		pcm[slot] = append(pcm[slot], out[:]...)
	})
	dec.SetFreqOffsetHz(afcHz)
	dec.Process(iq)

	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	for s := range pcm {
		if frames[s] == 0 {
			continue
		}
		out := filepath.Join(outDir, fmt.Sprintf("%s.slot%d.wav", base, s))
		if err := os.WriteFile(out, wav(pcm[s]), 0o644); err != nil {
			return err
		}
		slog.Info("decoded", "slot", s, "frames", frames[s], "secs", float64(frames[s])/50,
			"avg_bit_errs", float64(errs[s])/float64(max(frames[s]-bad[s], 1)),
			"uncorrectable", bad[s], "file", out)
	}
	st := dec.Stats()
	slog.Info("tdma", "syncs", st.Syncs, "slots", st.Slots, "isch_errs", st.ISCHErrors, "afc_hz", int(dec.FreqOffsetHz()))
	return nil
}

// wav wraps 8 kHz mono 16-bit PCM in a RIFF header.
func wav(pcm []int16) []byte {
	b := make([]byte, 44, 44+2*len(pcm))
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+2*len(pcm)))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1) // PCM
	binary.LittleEndian.PutUint16(b[22:], 1) // mono
	binary.LittleEndian.PutUint32(b[24:], p25.AudioSampleRate)
	binary.LittleEndian.PutUint32(b[28:], p25.AudioSampleRate*2)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(2*len(pcm)))
	for _, s := range pcm {
		b = binary.LittleEndian.AppendUint16(b, uint16(s))
	}
	return b
}
