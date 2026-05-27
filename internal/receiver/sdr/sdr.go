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

import "context"

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
	// ReadIQ starts the async IQ read loop. It returns a channel of
	// interleaved unsigned 8-bit IQ samples (I,Q,I,Q,...) which is the
	// native RTL-SDR format. The loop runs until ctx is cancelled or the
	// device errors out.
	ReadIQ(ctx context.Context, bufSize int) (<-chan []byte, error)
	// Close releases the dongle.
	Close() error
}
