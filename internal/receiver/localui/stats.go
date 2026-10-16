package localui

import (
	"sync"
	"time"

	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// StatsPoint is one stats frame with the time it was collected.
type StatsPoint struct {
	TS int64 `json:"ts"` // unix ms
	rxproto.Stats
}

const (
	statsWindow    = 24 * time.Hour
	maxStatsPoints = 720
)

// StatsHistory keeps the last 24 h of stats frames in memory.
type StatsHistory struct {
	mu     sync.Mutex
	points []StatsPoint // oldest first
}

// Add records a frame and drops anything older than the window.
func (h *StatsHistory) Add(p StatsPoint) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.points = append(h.points, p)
	cutoff := p.TS - statsWindow.Milliseconds()
	i := 0
	for i < len(h.points) && h.points[i].TS < cutoff {
		i++
	}
	if i > 0 {
		h.points = append(h.points[:0:0], h.points[i:]...)
	}
}

// Latest returns the newest frame, or nil.
func (h *StatsHistory) Latest() *StatsPoint {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.points) == 0 {
		return nil
	}
	p := h.points[len(h.points)-1]
	return &p
}

// Window returns the frames newer than now-window, merged into at most
// maxStatsPoints buckets, and the bucket length in seconds.
func (h *StatsHistory) Window(now time.Time, window time.Duration) ([]StatsPoint, int) {
	h.mu.Lock()
	cutoff := now.Add(-window).UnixMilli()
	var pts []StatsPoint
	for _, p := range h.points {
		if p.TS >= cutoff {
			pts = append(pts, p)
		}
	}
	h.mu.Unlock()

	if len(pts) <= maxStatsPoints {
		interval := 0
		if len(pts) > 0 {
			interval = pts[len(pts)-1].IntervalS
		}
		return pts, interval
	}
	per := (len(pts) + maxStatsPoints - 1) / maxStatsPoints
	out := make([]StatsPoint, 0, maxStatsPoints)
	for i := 0; i < len(pts); i += per {
		out = append(out, merge(pts[i:min(i+per, len(pts))]))
	}
	// The newest bucket may be partial; the first is always full.
	return out, out[0].IntervalS
}

// merge folds consecutive frames into one: counters summed, gauges
// averaged, flags and the latest identity fields taken from the last frame.
// Channels are matched by label; a channel missing from some frames is
// averaged over the frames it appears in.
func merge(ps []StatsPoint) StatsPoint {
	last := ps[len(ps)-1]
	out := StatsPoint{TS: last.TS, Stats: last.Stats}
	n := float64(len(ps))
	out.IntervalS = 0
	out.Dongle.IQBuffers, out.Dongle.IQDropped, out.Dongle.Gaps = 0, 0, 0
	out.Dongle.DSPLoad, out.Host.CPUTempC, out.Host.Load1 = 0, 0, 0
	var rtt float64
	for _, p := range ps {
		out.IntervalS += p.IntervalS
		out.Dongle.IQBuffers += p.Dongle.IQBuffers
		out.Dongle.IQDropped += p.Dongle.IQDropped
		out.Dongle.Gaps += p.Dongle.Gaps
		out.Dongle.DSPLoad += p.Dongle.DSPLoad / n
		out.Host.CPUTempC += p.Host.CPUTempC / n
		out.Host.Load1 += p.Host.Load1 / n
		out.Host.UndervoltageNow = out.Host.UndervoltageNow || p.Host.UndervoltageNow
		rtt += float64(p.Link.RTTMs) / n
	}
	out.Link.RTTMs = int(rtt + 0.5)

	type acc struct {
		c    rxproto.ChannelStats
		seen float64
	}
	byLabel := map[string]*acc{}
	var order []string
	for _, p := range ps {
		for _, c := range p.Channels {
			a := byLabel[c.Label]
			if a == nil {
				a = &acc{c: rxproto.ChannelStats{Label: c.Label, FreqHz: c.FreqHz}}
				byLabel[c.Label] = a
				order = append(order, c.Label)
			}
			a.seen++
			a.c.PowerDBFS += c.PowerDBFS
			a.c.NoiseFloorDBFS += c.NoiseFloorDBFS
			a.c.SNRDB += c.SNRDB
			a.c.CarrierPct += c.CarrierPct
			a.c.SyncLocks += c.SyncLocks
			a.c.SyncLosses += c.SyncLosses
			a.c.CWClean += c.CWClean
			a.c.CWCorrected += c.CWCorrected
			a.c.CWUncorrectable += c.CWUncorrectable
			a.c.CWChased += c.CWChased
			a.c.Pages += c.Pages
			a.c.PagesTruncated += c.PagesTruncated
			a.c.LastPageAgeS = c.LastPageAgeS
		}
	}
	out.Channels = make([]rxproto.ChannelStats, 0, len(order))
	for _, l := range order {
		a := byLabel[l]
		a.c.PowerDBFS /= a.seen
		a.c.NoiseFloorDBFS /= a.seen
		a.c.SNRDB /= a.seen
		a.c.CarrierPct /= a.seen
		out.Channels = append(out.Channels, a.c)
	}
	return out
}
