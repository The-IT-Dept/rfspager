package p25

// Websocket payloads a P25 receiver sends to the server, shared by
// internal/receiver/wsclient (sender) and internal/recvhub (receiver) so the
// schema lives in one place. Frame types:
//
//	p25.site       — SitePayload, on first decode and whenever it changes
//	p25.call_start — CallPayload with EndMS = 0
//	p25.call_end   — CallPayload, same CallID, EndMS set
//	p25.unit       — UnitPayload (affiliation / registration / deregistration)
const (
	FrameSite      = "p25.site"
	FrameCallStart = "p25.call_start"
	FrameCallEnd   = "p25.call_end"
	FrameUnit      = "p25.unit"
)

// SitePayload identifies the site a receiver is decoding, with what it
// advertises: its channel plan and neighbours.
type SitePayload struct {
	WACN      uint32         `json:"wacn"`
	SysID     uint16         `json:"sysid"`
	RFSS      uint8          `json:"rfss"`
	Site      uint8          `json:"site"`
	NAC       uint16         `json:"nac"`
	ControlHz uint64         `json:"cc_hz"`
	Neighbors []NeighborSite `json:"neighbors,omitempty"`
	Plan      []PlanEntry    `json:"plan,omitempty"`
}

type NeighborSite struct {
	SysID     uint16 `json:"sysid"`
	RFSS      uint8  `json:"rfss"`
	Site      uint8  `json:"site"`
	ControlHz uint64 `json:"cc_hz"`
}

type PlanEntry struct {
	ID        uint8  `json:"id"`
	BaseHz    uint64 `json:"base_hz"`
	SpacingHz uint64 `json:"spacing_hz"`
	TxOffset  int64  `json:"tx_offset_hz"`
	Bandwidth uint64 `json:"bandwidth_hz"`
	Slots     int    `json:"slots"`
}

// CallPayload is a voice call as seen on the control channel. CallID is
// assigned by the receiver and ties call_start to call_end.
type CallPayload struct {
	CallID    string `json:"call_id"`
	SysID     uint16 `json:"sysid"`
	RFSS      uint8  `json:"rfss"`
	Site      uint8  `json:"site"`
	Group     uint16 `json:"tg,omitempty"`     // talkgroup; 0 for private calls
	Target    uint32 `json:"target,omitempty"` // private calls only
	Source    uint32 `json:"src,omitempty"`    // 0 if only updates were seen
	FreqHz    uint64 `json:"freq_hz,omitempty"`
	Slot      int    `json:"slot,omitempty"`
	Encrypted bool   `json:"encrypted,omitempty"`
	Emergency bool   `json:"emergency,omitempty"`
	Patch     bool   `json:"patch,omitempty"`
	StartMS   int64  `json:"start_ms"`
	EndMS     int64  `json:"end_ms,omitempty"`
}

// Unit event kinds.
const (
	UnitKindAffiliation    = "affiliation"
	UnitKindRegistration   = "registration"
	UnitKindDeregistration = "deregistration"
)

// UnitPayload is a radio joining a talkgroup, or registering on / leaving
// the system.
type UnitPayload struct {
	Kind   string `json:"kind"`
	SysID  uint16 `json:"sysid"`
	RFSS   uint8  `json:"rfss"`
	Site   uint8  `json:"site"`
	Unit   uint32 `json:"unit"`
	Group  uint16 `json:"tg,omitempty"`
	Result uint8  `json:"result,omitempty"` // 0 = accepted
	TSMS   int64  `json:"ts_ms"`
}
