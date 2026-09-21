// Package sdr is the glue between the receiver agent and librtlsdr.
//
// The cgo-backed implementation lives in sdr_rtlsdr.go behind the `rtlsdr`
// build tag — that way `go build ./...` works on machines without librtlsdr
// installed, and only the receiver binary needs the native library.
//
// Build the receiver with:
//
//	go build -tags rtlsdr ./cmd/receiver
package sdr

import (
	"context"
	"sync"
)

// Buffer is one chunk of interleaved unsigned 8-bit IQ samples (I,Q,I,Q,…),
// the native RTL-SDR format, as delivered by Device.ReadIQ.
type Buffer struct {
	IQ []byte
	// DroppedBefore counts buffers the SDR layer had to discard immediately
	// before this one because the consumer was behind. Non-zero means the
	// sample stream has a hole of unknown length: decoders must resync
	// rather than treat the next bits as contiguous.
	DroppedBefore int
}

// bufPool recycles IQ storage between the librtlsdr callback (producer) and
// the DSP loop (consumer) so the ~120 buffers/s don't churn the GC.
var bufPool sync.Pool

func getBuf(n int) []byte {
	if p, ok := bufPool.Get().(*[]byte); ok && cap(*p) >= n {
		return (*p)[:n]
	}
	return make([]byte, n)
}

func putBuf(b []byte) { bufPool.Put(&b) }

// Recycle returns a buffer's storage to the pool. Call it once the IQ bytes
// have been consumed (e.g. after dsp.IQToComplex); b.IQ must not be used
// afterwards.
func Recycle(b Buffer) {
	if b.IQ != nil {
		putBuf(b.IQ)
	}
}

// DongleInfo is the per-dongle nameplate info we ship up to the server in
// the hello frame, so the admin portal can show what receive hardware is
// running on each agent.
type DongleInfo struct {
	Index        int    `json:"index"`
	Name         string `json:"name"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Product      string `json:"product,omitempty"`
	Serial       string `json:"serial,omitempty"`
}

// Device represents one open RTL-SDR dongle.
type Device interface {
	// Index returns the dongle's USB enumeration index.
	Index() int
	// Name is a human-readable identifier (manufacturer + serial).
	Name() string
	// Tune sets the centre frequency in Hz.
	Tune(hz uint32) error
	// SetSampleRate sets the ADC rate in Hz.
	SetSampleRate(hz uint32) error
	// SetGain sets the tuner gain in tenths of a dB; 0 enables AGC.
	SetGain(tenthsDB int) error
	// SetPPM sets the frequency correction in PPM.
	SetPPM(ppm int) error
	// ReadIQ starts the async IQ read loop. It returns a channel of IQ
	// buffers; each reports how many buffers were dropped just before it.
	// The loop runs until ctx is cancelled or the device errors out.
	// Consumers should hand buffers back via Recycle when done.
	ReadIQ(ctx context.Context, bufSize int) (<-chan Buffer, error)
	// Close releases the dongle.
	Close() error
}
