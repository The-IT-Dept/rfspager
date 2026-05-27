// Package recvhub holds the server-side websocket hub that talks to receiver
// agents. It owns the JSON frame schema, the per-pod in-memory conn map, and
// the cross-pod NOTIFY plumbing that lets any pod push config to the pod
// holding a given receiver's socket.
package recvhub

import "encoding/json"

// Frame is the discriminated-union envelope for every websocket message.
// We always Marshal a Frame and the receiver checks Type first.
type Frame struct {
	Type string          `json:"type"`
	ID   string          `json:"id,omitempty"`   // for request/ack correlation
	Data json.RawMessage `json:"data,omitempty"` // type-specific payload
}

// --- Client → server payloads ---

type DongleInfo struct {
	Index        int    `json:"index"`
	Name         string `json:"name"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Product      string `json:"product,omitempty"`
	Serial       string `json:"serial,omitempty"`
	Tuner        string `json:"tuner,omitempty"`
}

type HelloPayload struct {
	Version  string       `json:"version"`
	OS       string       `json:"os"`
	Arch     string       `json:"arch"`
	Hostname string       `json:"hostname"`
	Dongles  []DongleInfo `json:"dongles"`
}

type PagePayload struct {
	Address  string `json:"address"`
	Message  string `json:"message"`
	Datetime int64  `json:"datetime"` // unix seconds
	Function int    `json:"fn"`
	BitRate  int    `json:"baud"`
}

// SpectrumPayload is one waterfall slice: power in dB for each output bin,
// ordered low-frequency-first across (centre_hz ± sample_rate/2).
type SpectrumPayload struct {
	CentreHz   uint32    `json:"centre_hz"`
	SampleRate uint32    `json:"sample_rate"`
	Bins       []float32 `json:"bins"`
}

// --- Server → client payloads ---

type WelcomePayload struct {
	SystemID    int64  `json:"id"`
	SystemUUID  string `json:"system_id"`
	Name        string `json:"name"`
	PingSeconds int    `json:"ping_seconds"` // server's expected ping interval
}

type AckPayload struct {
	Status string `json:"status"` // "ok", "dup", "err"
	Error  string `json:"error,omitempty"`
}

type ConfigPayload struct {
	System map[string]any `json:"system"`
}
