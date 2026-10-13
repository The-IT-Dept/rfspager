package main

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/the-it-dept/rfspager/internal/receiver/pocsag"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

const (
	testMsg  = "21 September 2026 19:24:16 LHPATER7 - 26-128802 - BBQ fire/Bonfire/Yard fire - FIRECALL - 12 SMITH ST,PATERSON"
	testAddr = 11416
	baud     = 512
	devHz    = 4500.0 // POCSAG deviation the receiver's slicers are built for
)

// synthAudio renders bits as the FM demod output a receiver would record:
// NRZ at ±2π·dev/rate rad/sample, scaled to int16 as the audio capture
// does, followed by a second of silence so the decoder sees the sync word
// fail to return and releases the last page.
func synthAudio(bits []bool, rate uint32) []byte {
	spb := float64(rate) / baud
	dev := 2 * math.Pi * devHz / float64(rate)
	total := int(float64(len(bits))*spb) + int(rate)
	out := make([]byte, 0, 2*total)
	for i := range total {
		var v float64
		if b := int(float64(i) / spb); b < len(bits) {
			v = -dev
			if bits[b] {
				v = dev
			}
		}
		s := int16(math.Round(v * 32767 / math.Pi))
		out = binary.LittleEndian.AppendUint16(out, uint16(s))
	}
	return out
}

// synthIQ renders the same burst as complex baseband: a 0.5 amplitude
// carrier whose frequency is ±dev, as the iq capture would record it.
func synthIQ(bits []bool, rate uint32) []byte {
	spb := float64(rate) / baud
	total := int(float64(len(bits))*spb) + int(rate)
	out := make([]byte, 0, 4*total)
	var phase float64
	for i := range total {
		var re, im float64
		if b := int(float64(i) / spb); b < len(bits) {
			f := -devHz
			if bits[b] {
				f = devHz
			}
			phase += 2 * math.Pi * f / float64(rate)
			re, im = 0.5*math.Cos(phase), 0.5*math.Sin(phase)
		}
		out = binary.LittleEndian.AppendUint16(out, uint16(int16(math.Round(re*32767))))
		out = binary.LittleEndian.AppendUint16(out, uint16(int16(math.Round(im*32767))))
	}
	return out
}

func TestReplayRoundTrip(t *testing.T) {
	bits := pocsag.EncodeTransmission(testAddr, 0, testMsg)
	for kind, data := range map[string][]byte{
		rxproto.CaptureAudio: synthAudio(bits, rxproto.AudioRate),
		rxproto.CaptureIQ:    synthIQ(bits, rxproto.AudioRate),
	} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "capture."+kind)
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			var out bytes.Buffer
			st, err := replay(kind, rxproto.AudioRate, f, &out)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "0011416 fn=0 baud=512") || !strings.Contains(out.String(), testMsg) {
				t.Fatalf("page not decoded; output:\n%s", out.String())
			}
			if st.Pages != 1 || st.CWUncorrectable != 0 || st.SyncLocks != 1 {
				t.Errorf("stats %+v", st)
			}
			var summary bytes.Buffer
			printStats(&summary, st)
			if !strings.Contains(summary.String(), "pages=1") {
				t.Errorf("summary %q", summary.String())
			}
		})
	}
}

func TestReplayRejectsUnknownKind(t *testing.T) {
	if _, err := replay("video", 38400, bytes.NewReader(make([]byte, 8)), &bytes.Buffer{}); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := replay("audio", 0, bytes.NewReader(nil), &bytes.Buffer{}); err == nil {
		t.Fatal("expected an error for rate 0")
	}
}
