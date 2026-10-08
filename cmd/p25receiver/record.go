package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/the-it-dept/rfspager/internal/p25"
)

// recorder writes the baseband of every clear call whose voice channel falls
// inside the dongle's capture bandwidth, as 48 kS/s complex float32 files —
// raw material for building and testing the voice decoder.
type recorder struct {
	dir    string
	center uint32
	active map[string]*recording
}

type recording struct {
	ch   *p25.Channelizer
	f    *os.File
	w    *bufio.Writer
	path string
}

// recordSpan is how far from the dongle centre a channel may sit and still
// be clean: inside the stage-1 filter's passband at 2.4 MS/s, clear of the
// band edges.
const recordSpan = 1_000_000

func newRecorder(dir string) *recorder {
	return &recorder{dir: dir, active: map[string]*recording{}}
}

// retune closes everything; the new centre applies to calls from now on.
func (r *recorder) retune(center uint32) {
	for id := range r.active {
		r.stop(id)
	}
	r.center = center
}

func (r *recorder) call(frameType string, c p25.CallPayload) {
	if frameType == p25.FrameCallEnd {
		r.stop(c.CallID)
		return
	}
	off := int64(c.FreqHz) - int64(r.center)
	if c.Encrypted || c.FreqHz == 0 || off > recordSpan || off < -recordSpan || abs(off) < 20_000 {
		return
	}
	name := fmt.Sprintf("%s_tg%d_src%d_%.4f_s%d.cf32",
		time.UnixMilli(c.StartMS).Format("20060102T150405"), c.Group, c.Source, float64(c.FreqHz)/1e6, c.Slot)
	path := filepath.Join(r.dir, name)
	f, err := os.Create(path)
	if err != nil {
		slog.Warn("record", "err", err)
		return
	}
	r.active[c.CallID] = &recording{
		ch: p25.NewChannelizer(float64(off), inputRate), f: f, w: bufio.NewWriterSize(f, 1<<16), path: path,
	}
	slog.Info("recording", "file", name)
}

func (r *recorder) process(wide []complex128) {
	var b [8]byte
	for _, rec := range r.active {
		for _, s := range rec.ch.Process(wide) {
			binary.LittleEndian.PutUint32(b[:4], math.Float32bits(float32(real(s))))
			binary.LittleEndian.PutUint32(b[4:], math.Float32bits(float32(imag(s))))
			_, _ = rec.w.Write(b[:])
		}
	}
}

func (r *recorder) stop(id string) {
	rec, ok := r.active[id]
	if !ok {
		return
	}
	delete(r.active, id)
	_ = rec.w.Flush()
	_ = rec.f.Close()
	slog.Info("recorded", "file", filepath.Base(rec.path))
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
