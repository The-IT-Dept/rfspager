//go:build rtlsdr

package sdr

import (
	"context"
	"fmt"
	"log/slog"

	rtl "github.com/jpoirier/gortlsdr"
)

// Open opens a dongle by index. Returns an error if the dongle can't be opened.
func Open(index int) (Device, error) {
	dev, err := rtl.Open(index)
	if err != nil {
		return nil, fmt.Errorf("opening rtl-sdr dongle %d: %w", index, err)
	}
	m, p, s, _ := rtl.GetDeviceUsbStrings(index)
	name := fmt.Sprintf("%s %s (%s)", m, p, s)
	return &device{dev: dev, index: index, name: name}, nil
}

// Count returns the number of attached RTL-SDR dongles.
func Count() int {
	return rtl.GetDeviceCount()
}

// Enumerate returns nameplate info for every attached dongle, including ones
// we don't open. Used for the receiver's hello frame.
func Enumerate() []DongleInfo {
	n := rtl.GetDeviceCount()
	out := make([]DongleInfo, 0, n)
	for i := 0; i < n; i++ {
		m, p, s, _ := rtl.GetDeviceUsbStrings(i)
		out = append(out, DongleInfo{
			Index:        i,
			Name:         rtl.GetDeviceName(i),
			Manufacturer: m,
			Product:      p,
			Serial:       s,
		})
	}
	return out
}

type device struct {
	dev   *rtl.Context
	index int
	name  string
}

func (d *device) Index() int   { return d.index }
func (d *device) Name() string { return d.name }

func (d *device) Tune(hz uint32) error {
	if err := d.dev.SetCenterFreq(int(hz)); err != nil {
		return fmt.Errorf("tune %d: %w", hz, err)
	}
	return nil
}

func (d *device) SetSampleRate(hz uint32) error {
	if err := d.dev.SetSampleRate(int(hz)); err != nil {
		return fmt.Errorf("set sample rate %d: %w", hz, err)
	}
	return nil
}

func (d *device) SetGain(tenthsDB int) error {
	if tenthsDB <= 0 {
		// AGC: hand control back to the tuner + RTL2832 IF.
		if err := d.dev.SetTunerGainMode(false); err != nil {
			return fmt.Errorf("enable agc: %w", err)
		}
		_ = d.dev.SetAgcMode(true)
		return nil
	}
	if err := d.dev.SetTunerGainMode(true); err != nil {
		return fmt.Errorf("manual gain mode: %w", err)
	}
	if err := d.dev.SetTunerGain(tenthsDB); err != nil {
		return fmt.Errorf("set gain %d: %w", tenthsDB, err)
	}
	return nil
}

func (d *device) SetPPM(ppm int) error {
	if ppm == 0 {
		return nil
	}
	if err := d.dev.SetFreqCorrection(ppm); err != nil {
		return fmt.Errorf("set ppm %d: %w", ppm, err)
	}
	return nil
}

func (d *device) ReadIQ(ctx context.Context, bufSize int) (<-chan Buffer, error) {
	if err := d.dev.ResetBuffer(); err != nil {
		return nil, fmt.Errorf("reset buffer: %w", err)
	}
	// Queue ~270ms of IQ at 1.92 MS/s to absorb GC pauses + scheduling
	// jitter. The callback below NEVER blocks (drops on overflow) so a
	// consumer stall can't back-pressure into libusb's transfer queue and
	// stall the dongle. Drops are reported on the next buffer that does get
	// through so the consumer can resync its decoders.
	out := make(chan Buffer, 32)
	var droppedTotal uint64
	// Drops since the last successful hand-off. librtlsdr invokes the
	// callback from its single event-handling thread, so no atomics needed.
	pending := 0
	cb := func(buf []byte) {
		// gortlsdr hands us a slice aliasing libusb's transfer buffer, which
		// is reused as soon as we return — copy before handing off.
		b := getBuf(len(buf))
		copy(b, buf)
		select {
		case out <- Buffer{IQ: b, DroppedBefore: pending}:
			pending = 0
		default:
			// Consumer is behind. Drop and keep libusb fed — better to lose
			// 8ms of samples than to stall the USB pipe and force a full
			// dongle re-open.
			pending++
			droppedTotal++
			putBuf(b)
			if droppedTotal == 1 || droppedTotal%128 == 0 {
				slog.Warn("iq buffer overflow, dropping",
					"system", "sdr",
					"dropped_total", droppedTotal,
				)
			}
		}
	}
	go func() {
		defer close(out)
		// ReadAsync blocks; cancel by calling CancelAsync from another goroutine.
		errCh := make(chan error, 1)
		go func() { errCh <- d.dev.ReadAsync(cb, nil, 0, bufSize) }()
		select {
		case <-ctx.Done():
			_ = d.dev.CancelAsync()
			<-errCh
		case <-errCh:
		}
	}()
	return out, nil
}

func (d *device) Close() error { return d.dev.Close() }
