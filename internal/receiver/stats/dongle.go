package stats

import (
	"sync/atomic"
	"time"
)

// Dongle accumulates the IQ loop's counters for one stats interval. The
// dongle loop adds to it per buffer; the stats goroutine takes Snapshot,
// which resets the interval counters.
type Dongle struct {
	buffers   atomic.Int64
	dropped   atomic.Int64
	gaps      atomic.Int64
	procNanos atomic.Int64 // time spent in Channel.Process, all channels
	samples   atomic.Int64 // IQ samples those buffers carried
}

// Buffer records one delivered IQ buffer: how many buffers the SDR layer
// dropped just before it, how many complex samples it carried, and how long
// the channels took to process it.
func (d *Dongle) Buffer(droppedBefore, samples int, processing time.Duration) {
	d.buffers.Add(1)
	if droppedBefore > 0 {
		d.dropped.Add(int64(droppedBefore))
		d.gaps.Add(1)
	}
	d.samples.Add(int64(samples))
	d.procNanos.Add(processing.Nanoseconds())
}

// Snapshot returns the interval's buffers, drops, gaps and DSP load (time
// in Process divided by the real-time duration of the IQ consumed; 1.0
// means the receiver is only just keeping up) and resets the counters.
func (d *Dongle) Snapshot(sampleRate uint32) (buffers, dropped, gaps int, dspLoad float64) {
	buffers = int(d.buffers.Swap(0))
	dropped = int(d.dropped.Swap(0))
	gaps = int(d.gaps.Swap(0))
	proc := d.procNanos.Swap(0)
	samples := d.samples.Swap(0)
	if samples > 0 && sampleRate > 0 {
		wall := float64(samples) / float64(sampleRate) * 1e9
		dspLoad = float64(proc) / wall
	}
	return
}
