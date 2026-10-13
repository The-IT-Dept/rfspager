// Command replay runs an audio or iq capture (as uploaded by a receiver's
// capture command, see internal/receiver/control/capture.go) through the
// same FM demodulator, bit slicers and POCSAG decoders the receiver uses,
// prints every page it finds and the decoder counters at the end. It lets
// DSP changes be tried against real signal on a laptop.
//
//	replay -kind audio -rate 38400 newcastle-rfs.audio
//	replay -kind iq    -rate 38400 newcastle-rfs.iq
//
// audio: int16 little-endian FM demod samples, ±32767 = ±pi rad/sample.
// iq:    interleaved int16 little-endian I,Q baseband, 32767 = 1.0.
package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/the-it-dept/rfspager/internal/receiver"
	"github.com/the-it-dept/rfspager/internal/receiver/pocsag"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

func main() {
	kind := flag.String("kind", rxproto.CaptureAudio, "capture kind: audio or iq")
	rate := flag.Uint("rate", uint(rxproto.AudioRate), "sample rate of the capture in Hz")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: replay -kind audio|iq [-rate 38400] <file>\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	f, err := os.Open(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	st, err := replay(*kind, uint32(*rate), bufio.NewReader(f), os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	printStats(os.Stdout, st)
}

// replay decodes r, printing one line per page to w, and returns the
// summed decoder counters.
func replay(kind string, rate uint32, r io.Reader, w io.Writer) (pocsag.Stats, error) {
	if rate == 0 {
		return pocsag.Stats{}, fmt.Errorf("rate must be positive")
	}
	pages := make(chan pocsag.Page, 64)
	dec := receiver.NewDecoders(rate, pages)
	drain := func() {
		for {
			select {
			case p := <-pages:
				fmt.Fprintf(w, "%s fn=%d baud=%d %q\n", p.Address, p.Function, p.BitRate, pageText(p))
			default:
				return
			}
		}
	}

	var buf [4]byte
	var iqOne [1]complex128
	var n int
	for {
		var demod float64
		switch kind {
		case rxproto.CaptureAudio:
			if _, err := io.ReadFull(r, buf[:2]); err != nil {
				if err == io.EOF || err == io.ErrUnexpectedEOF {
					drain()
					return dec.Stats(), nil
				}
				return dec.Stats(), err
			}
			demod = float64(int16(binary.LittleEndian.Uint16(buf[:2]))) * math.Pi / 32767
		case rxproto.CaptureIQ:
			if _, err := io.ReadFull(r, buf[:4]); err != nil {
				if err == io.EOF || err == io.ErrUnexpectedEOF {
					drain()
					return dec.Stats(), nil
				}
				return dec.Stats(), err
			}
			re := float64(int16(binary.LittleEndian.Uint16(buf[:2]))) / 32767
			im := float64(int16(binary.LittleEndian.Uint16(buf[2:4]))) / 32767
			// IQ goes through the production path: per-baud IF filter and
			// discriminator inside the legs.
			iqOne[0] = complex(re, im)
			dec.FeedIQ(iqOne[:])
			if n++; n%1024 == 0 {
				drain()
			}
			continue
		default:
			return pocsag.Stats{}, fmt.Errorf("unknown kind %q (want audio or iq)", kind)
		}
		dec.Feed(demod)
		if n++; n%1024 == 0 {
			drain()
		}
	}
}

func pageText(p pocsag.Page) string {
	if p.Alpha != "" {
		return p.Alpha
	}
	return p.Numeric
}

func printStats(w io.Writer, st pocsag.Stats) {
	fmt.Fprintf(w, "sync locks=%d losses=%d | codewords clean=%d corrected=%d uncorrectable=%d | pages=%d truncated=%d\n",
		st.SyncLocks, st.SyncLosses, st.CWClean, st.CWCorrected, st.CWUncorrectable, st.Pages, st.PagesTruncated)
}
