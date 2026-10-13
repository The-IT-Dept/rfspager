// Package rxproto is the wire contract between a receiver agent and the
// server's receiver hub. Both sides import it so the frame payloads and the
// runtime config schema cannot drift apart.
//
// Frames are JSON envelopes {type, id, data}. Protocol version 1 is what
// receivers 5.2.x/5.3.x speak: hello, page, log, ping up; welcome, pong,
// ack, config down (the config frame was logged and ignored). Version 2
// adds stats, config_result and command_result up, and typed config plus
// command down. The server only sends v2 frames to receivers that declared
// proto >= 2 in their hello.
package rxproto

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// ProtoVersion is the protocol version this package describes.
const ProtoVersion = 2

// AudioRate is the per-channel sample rate after decimation. Every
// supported dongle sample rate must be an integer multiple of it.
const AudioRate uint32 = 38_400

// Frame types, receiver -> server.
const (
	FrameHello         = "hello"
	FramePage          = "page"
	FrameLog           = "log"
	FramePing          = "ping"
	FrameStats         = "stats"
	FrameConfigResult  = "config_result"
	FrameCommandResult = "command_result"
)

// Frame types, server -> receiver.
const (
	FrameWelcome = "welcome"
	FramePong    = "pong"
	FrameAck     = "ack"
	FrameConfig  = "config"
	FrameCommand = "command"
)

// Frame is the envelope for every message in both directions.
type Frame struct {
	Type string          `json:"type"`
	ID   string          `json:"id,omitempty"`   // request/ack and command correlation
	Data json.RawMessage `json:"data,omitempty"` // type-specific payload
}

// DongleInfo describes one RTL-SDR device the receiver can see.
type DongleInfo struct {
	Index        int    `json:"index"`
	Name         string `json:"name"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Product      string `json:"product,omitempty"`
	Serial       string `json:"serial,omitempty"`
	Tuner        string `json:"tuner,omitempty"`
}

// HostInfo is the slow-changing description of the box the receiver runs on.
type HostInfo struct {
	Model   string `json:"model,omitempty"`  // e.g. "Raspberry Pi 4 Model B Rev 1.5"
	Kernel  string `json:"kernel,omitempty"` // uname -r
	UptimeS int64  `json:"uptime_s,omitempty"`
	RTLSDR  string `json:"rtlsdr,omitempty"` // installed librtlsdr package(s), e.g. "librtlsdr0 0.6.0-4" or "rtl-sdr-blog 1.3.6"
}

// HelloConfig tells the server which config the receiver is running when it
// connects. Version 0 means "built-in defaults / env bootstrap, nothing from
// the server yet". The server seeds revision 1 from Effective when the
// system has no revisions.
type HelloConfig struct {
	Version   int           `json:"version"`
	Effective RuntimeConfig `json:"effective"`
}

// Hello is the first frame a receiver sends.
type Hello struct {
	Version  string       `json:"version"`
	OS       string       `json:"os"`
	Arch     string       `json:"arch"`
	Hostname string       `json:"hostname"`
	Dongles  []DongleInfo `json:"dongles"`
	Proto    int          `json:"proto,omitempty"` // absent/0 = v1
	Host     *HostInfo    `json:"host,omitempty"`
	Config   *HelloConfig `json:"config,omitempty"`
}

// Welcome is the server's reply to hello.
type Welcome struct {
	SystemID      int64          `json:"id"`
	SystemUUID    string         `json:"system_id"`
	Name          string         `json:"name"`
	PingSeconds   int            `json:"ping_seconds"`
	Config        *ConfigPayload `json:"config,omitempty"`         // v2 only
	LatestVersion string         `json:"latest_version,omitempty"` // newest published receiver release, if known
}

// Page is one decoded POCSAG page.
type Page struct {
	Address  string `json:"address"`
	Message  string `json:"message"`
	Datetime int64  `json:"datetime"` // unix seconds
	Function int    `json:"fn"`
	BitRate  int    `json:"baud"`
}

// Log is one shipped log line.
type Log struct {
	TS      int64          `json:"ts"` // unix milliseconds, receiver clock
	Level   string         `json:"level,omitempty"`
	Source  string         `json:"source,omitempty"`
	Message string         `json:"message"`
	Raw     map[string]any `json:"raw,omitempty"`
}

// Ack answers a page frame.
type Ack struct {
	Status string `json:"status"` // ok | dup | err
	Error  string `json:"error,omitempty"`
}

// ConfigPayload carries one config revision down to the receiver.
type ConfigPayload struct {
	Version int           `json:"version"`
	Config  RuntimeConfig `json:"config"`
}

// Config result statuses.
const (
	ConfigApplied  = "applied"
	ConfigRejected = "rejected"
)

// ConfigResult reports what the receiver did with a config revision.
type ConfigResult struct {
	Version   int               `json:"version"`
	Status    string            `json:"status"`
	Errors    map[string]string `json:"errors,omitempty"` // key -> reason, when rejected
	Effective RuntimeConfig     `json:"effective"`        // what is actually running now
}

// Command types.
const (
	CmdRestart     = "restart"
	CmdDongleReset = "dongle_reset"
	CmdUpdate      = "update"
	CmdCapture     = "capture"
)

// Command asks the receiver to do one thing. Params depends on Type.
type Command struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	Params json.RawMessage `json:"params,omitempty"`
}

// UpdateParams is the params for CmdUpdate.
type UpdateParams struct {
	Version string `json:"version"` // release tag, e.g. "v5.5.0"
}

// Capture kinds.
const (
	CaptureSpectrum = "spectrum"
	CaptureAudio    = "audio"
	CaptureIQ       = "iq"
)

// MaxCaptureSeconds bounds audio/iq captures.
const MaxCaptureSeconds = 30

// CaptureParams is the params for CmdCapture.
type CaptureParams struct {
	CaptureID string `json:"capture_id"`
	Kind      string `json:"kind"`
	Channel   string `json:"channel,omitempty"` // channel label; unused for spectrum
	Seconds   int    `json:"seconds,omitempty"` // unused for spectrum
}

// Command result statuses.
const (
	CmdProgress = "progress"
	CmdOK       = "ok"
	CmdError    = "error"
)

// CommandResult reports progress or completion of a command.
type CommandResult struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Status  string `json:"status"`
	Stage   string `json:"stage,omitempty"`
	Message string `json:"message,omitempty"`
}

// Stats is the periodic health report. Counters are deltas for the
// interval unless the name ends in Total.
type Stats struct {
	IntervalS int            `json:"interval_s"`
	Dongle    DongleStats    `json:"dongle"`
	Host      HostStats      `json:"host"`
	Link      LinkStats      `json:"link"`
	Channels  []ChannelStats `json:"channels"`
}

// DongleStats describes the SDR for the interval.
type DongleStats struct {
	Open         bool    `json:"open"`
	GainTenthsDB int     `json:"gain_tenths_db"`
	AGC          bool    `json:"agc"`
	PPM          int     `json:"ppm"`
	CentreHz     uint32  `json:"centre_hz"`
	SampleRate   uint32  `json:"sample_rate"`
	IQBuffers    int     `json:"iq_buffers"`
	IQDropped    int     `json:"iq_dropped"`
	Gaps         int     `json:"gaps"`
	DSPLoad      float64 `json:"dsp_load"` // processing time / buffer duration, 1.0 = falling behind
}

// HostStats describes the box for the interval.
type HostStats struct {
	CPUTempC         float64 `json:"cpu_temp_c"`
	Load1            float64 `json:"load1"`
	MemFreeMB        int     `json:"mem_free_mb"`
	ThrottledHex     string  `json:"throttled_hex,omitempty"` // Pi firmware get_throttled, "" when unavailable
	UndervoltageNow  bool    `json:"undervoltage_now"`
	UndervoltageEver bool    `json:"undervoltage_ever"`
	UptimeS          int64   `json:"uptime_s"`
}

// LinkStats describes the websocket link.
type LinkStats struct {
	ReconnectsTotal    int `json:"reconnects_total"`
	QueueDepth         int `json:"queue_depth"`
	DroppedFramesTotal int `json:"dropped_frames_total"`
	RTTMs              int `json:"rtt_ms"`
}

// ChannelStats describes one POCSAG channel for the interval.
type ChannelStats struct {
	Label           string  `json:"label"`
	FreqHz          uint32  `json:"freq_hz"`
	PowerDBFS       float64 `json:"power_dbfs"`       // p90 of block power
	NoiseFloorDBFS  float64 `json:"noise_floor_dbfs"` // p10 of block power
	SNRDB           float64 `json:"snr_db"`           // p90 - p10
	CarrierPct      float64 `json:"carrier_pct"`      // blocks > floor + 6 dB
	SyncLocks       int     `json:"sync_locks"`
	SyncLosses      int     `json:"sync_losses"`
	CWClean         int     `json:"cw_clean"`
	CWCorrected     int     `json:"cw_corrected"`
	CWUncorrectable int     `json:"cw_uncorrectable"`
	Pages           int     `json:"pages"`
	PagesTruncated  int     `json:"pages_truncated"`
	LastPageAgeS    int     `json:"last_page_age_s"` // -1 when no page since start
}

// ChannelConfig is one POCSAG frequency to decode.
type ChannelConfig struct {
	Label   string `json:"label"`
	FreqHz  uint32 `json:"freq_hz"`
	Enabled bool   `json:"enabled"`
}

// Log levels a receiver accepts.
var LogLevels = []string{"debug", "info", "warn"}

// RuntimeConfig is everything about a receiver the server controls. The
// zero value is not valid; start from Defaults().
type RuntimeConfig struct {
	TunerGainTenthsDB int             `json:"tuner_gain_tenths_db"` // 0 = AGC
	PPM               int             `json:"ppm"`
	SampleRate        uint32          `json:"sample_rate"`
	DongleSerial      string          `json:"dongle_serial"` // "" = pick by index
	DongleIndex       int             `json:"dongle_index"`  // -1 = first available
	PostAll           bool            `json:"post_all"`
	LogLevel          string          `json:"log_level"`
	StatsIntervalS    int             `json:"stats_interval_s"`
	Channels          []ChannelConfig `json:"channels"`
}

// Defaults reproduces the behaviour receivers had before remote config:
// AGC, 1.92 MS/s, first dongle, the two hardcoded NSW channels.
func Defaults() RuntimeConfig {
	return RuntimeConfig{
		TunerGainTenthsDB: 0,
		PPM:               0,
		SampleRate:        1_920_000,
		DongleSerial:      "",
		DongleIndex:       -1,
		PostAll:           false,
		LogLevel:          "info",
		StatsIntervalS:    15,
		Channels: []ChannelConfig{
			{Label: "RFS", FreqHz: 148_587_500, Enabled: true},
			{Label: "FRNSW", FreqHz: 148_962_500, Enabled: true},
		},
	}
}

// Normalize decodes a stored config over the defaults, so keys missing from
// older revisions take their default values. Channels, when present,
// replace the default list wholesale.
func Normalize(raw []byte) (RuntimeConfig, error) {
	cfg := Defaults()
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return RuntimeConfig{}, fmt.Errorf("decoding runtime config: %w", err)
	}
	return cfg, nil
}

// Validation limits.
const (
	MaxGainTenthsDB   = 600
	MaxPPM            = 200
	MinSampleRate     = 768_000
	MaxSampleRate     = 2_400_000
	MaxChannels       = 4
	MinStatsIntervalS = 5
	MaxStatsIntervalS = 300
	// usableBandwidth is the fraction of the sample rate channels may span;
	// the edges of an RTL-SDR capture roll off and alias.
	usableBandwidth = 0.8
)

// Validate returns a map of field -> problem, or nil when the config is
// acceptable. Keys use the JSON names so the admin UI can attach them to
// inputs; channel problems are keyed "channels[i].field".
func (c RuntimeConfig) Validate() map[string]string {
	errs := map[string]string{}
	if c.TunerGainTenthsDB < 0 || c.TunerGainTenthsDB > MaxGainTenthsDB {
		errs["tuner_gain_tenths_db"] = fmt.Sprintf("must be 0 (AGC) to %d", MaxGainTenthsDB)
	}
	if c.PPM < -MaxPPM || c.PPM > MaxPPM {
		errs["ppm"] = fmt.Sprintf("must be between -%d and %d", MaxPPM, MaxPPM)
	}
	switch {
	case c.SampleRate < MinSampleRate || c.SampleRate > MaxSampleRate:
		errs["sample_rate"] = fmt.Sprintf("must be between %d and %d", MinSampleRate, MaxSampleRate)
	case c.SampleRate%AudioRate != 0:
		errs["sample_rate"] = fmt.Sprintf("must be a multiple of %d", AudioRate)
	}
	if c.DongleIndex < -1 {
		errs["dongle_index"] = "must be -1 (first available) or a device index"
	}
	if !slices.Contains(LogLevels, c.LogLevel) {
		errs["log_level"] = "must be one of " + strings.Join(LogLevels, ", ")
	}
	if c.StatsIntervalS < MinStatsIntervalS || c.StatsIntervalS > MaxStatsIntervalS {
		errs["stats_interval_s"] = fmt.Sprintf("must be between %d and %d seconds", MinStatsIntervalS, MaxStatsIntervalS)
	}
	c.validateChannels(errs)
	if len(errs) == 0 {
		return nil
	}
	return errs
}

func (c RuntimeConfig) validateChannels(errs map[string]string) {
	if len(c.Channels) == 0 {
		errs["channels"] = "at least one channel is required"
		return
	}
	if len(c.Channels) > MaxChannels {
		errs["channels"] = fmt.Sprintf("at most %d channels", MaxChannels)
		return
	}
	seen := map[string]bool{}
	for i, ch := range c.Channels {
		label := strings.TrimSpace(ch.Label)
		switch {
		case label == "":
			errs[fmt.Sprintf("channels[%d].label", i)] = "label is required"
		case seen[label]:
			errs[fmt.Sprintf("channels[%d].label", i)] = "duplicate label"
		}
		seen[label] = true
		if ch.FreqHz < 24_000_000 || ch.FreqHz > 1_750_000_000 {
			errs[fmt.Sprintf("channels[%d].freq_hz", i)] = "must be within the RTL-SDR tuning range (24 MHz to 1.75 GHz)"
		}
	}
	enabled := c.EnabledChannels()
	if len(enabled) == 0 {
		errs["channels"] = "at least one channel must be enabled"
		return
	}
	lo, hi := enabled[0].FreqHz, enabled[0].FreqHz
	for _, ch := range enabled {
		if ch.FreqHz < lo {
			lo = ch.FreqHz
		}
		if ch.FreqHz > hi {
			hi = ch.FreqHz
		}
	}
	if c.SampleRate > 0 && float64(hi-lo) > usableBandwidth*float64(c.SampleRate) {
		errs["channels"] = fmt.Sprintf("enabled channels span %d Hz; at most %.0f Hz fits in a %d Hz capture",
			hi-lo, usableBandwidth*float64(c.SampleRate), c.SampleRate)
	}
}

// EnabledChannels returns the channels the receiver should decode.
func (c RuntimeConfig) EnabledChannels() []ChannelConfig {
	var out []ChannelConfig
	for _, ch := range c.Channels {
		if ch.Enabled {
			out = append(out, ch)
		}
	}
	return out
}

// CentreHz is the dongle centre frequency: the midpoint of the lowest and
// highest enabled channel. ok is false when nothing is enabled.
func (c RuntimeConfig) CentreHz() (centre uint32, ok bool) {
	enabled := c.EnabledChannels()
	if len(enabled) == 0 {
		return 0, false
	}
	lo, hi := enabled[0].FreqHz, enabled[0].FreqHz
	for _, ch := range enabled {
		if ch.FreqHz < lo {
			lo = ch.FreqHz
		}
		if ch.FreqHz > hi {
			hi = ch.FreqHz
		}
	}
	return lo + (hi-lo)/2, true
}

// LiveApplicable reports whether moving from old to new needs the dongle
// loop restarted (false) or can be applied to the running device (true).
func LiveApplicable(old, new RuntimeConfig) bool {
	if old.SampleRate != new.SampleRate || old.DongleSerial != new.DongleSerial || old.DongleIndex != new.DongleIndex {
		return false
	}
	if len(old.Channels) != len(new.Channels) {
		return false
	}
	for i := range old.Channels {
		if old.Channels[i] != new.Channels[i] {
			return false
		}
	}
	return true
}
