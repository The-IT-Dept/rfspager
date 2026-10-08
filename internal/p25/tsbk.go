package p25

import "fmt"

// TSBK is one CRC-valid 12-byte trunking signalling block, split into its
// header and 64-bit argument field (TIA-102.AABC).
type TSBK struct {
	LastBlock bool
	Protected bool
	Opcode    uint8
	MFID      uint8 // manufacturer ID: 0x00 standard, 0x90 Motorola
	Args      uint64
}

func parseTSBK(b [12]byte) TSBK {
	var args uint64
	for _, x := range b[2:10] {
		args = args<<8 | uint64(x)
	}
	return TSBK{
		LastBlock: b[0]&0x80 != 0,
		Protected: b[0]&0x40 != 0,
		Opcode:    b[0] & 0x3F,
		MFID:      b[1],
		Args:      args,
	}
}

// field extracts n bits starting `from` bits below the top of Args.
func (t TSBK) field(from, n uint) uint64 {
	return t.Args >> (64 - from - n) & (1<<n - 1)
}

// Channel is a 16-bit P25 channel reference: 4-bit identifier (which
// channel-plan entry) + 12-bit channel number. Resolve to a frequency with a
// ChannelPlan.
type Channel uint16

func (c Channel) Iden() uint8    { return uint8(c >> 12) }
func (c Channel) Number() uint16 { return uint16(c & 0xFFF) }
func (c Channel) String() string { return fmt.Sprintf("%X-%d", c.Iden(), c.Number()) }

// ServiceOptions accompanies voice grants.
type ServiceOptions uint8

func (s ServiceOptions) Emergency() bool { return s&0x80 != 0 }
func (s ServiceOptions) Encrypted() bool { return s&0x40 != 0 }
func (s ServiceOptions) Duplex() bool    { return s&0x20 != 0 }
func (s ServiceOptions) Packet() bool    { return s&0x10 != 0 }
func (s ServiceOptions) Priority() int   { return int(s & 0x07) }

// --- Decoded messages -----------------------------------------------------

// Message is any decoded control-channel message.
type Message interface{ isMessage() }

// GroupVoiceGrant assigns a voice channel to a talkgroup call. Source is 0
// on grant updates, which only repeat the channel for late joiners.
type GroupVoiceGrant struct {
	Options ServiceOptions
	Channel Channel
	Group   uint16
	Source  uint32
	Update  bool // from a GRP_V_CH_GRANT_UPDT rather than the initial grant
	Patch   bool // Motorola supergroup (patched talkgroups) grant
}

// UnitVoiceGrant assigns a voice channel to a private (unit-to-unit) call.
type UnitVoiceGrant struct {
	Options ServiceOptions
	Channel Channel
	Target  uint32
	Source  uint32
	Update  bool
}

// GroupAffiliation is the FNE's response to a radio joining a talkgroup.
type GroupAffiliation struct {
	Group             uint16
	AnnouncementGroup uint16
	Target            uint32
	Result            uint8 // 0 accepted, 1 failed, 2 denied, 3 refused
}

// UnitRegistration is the FNE's response to a radio registering on a site.
type UnitRegistration struct {
	SystemID uint16
	SourceID uint32
	Source   uint32
	Result   uint8
}

// UnitDeregistration acknowledges a radio leaving the system.
type UnitDeregistration struct {
	WACN     uint32
	SystemID uint16
	Source   uint32
}

// IdentifierUpdate is one channel-plan entry: frequencies for channels whose
// Iden() matches ID are Base + Spacing·(n / Slots).
type IdentifierUpdate struct {
	ID        uint8
	BaseHz    uint64
	SpacingHz uint64
	TxOffset  int64 // inbound (radio transmit) offset from the outbound
	Bandwidth uint64
	TDMA      bool
	Slots     int // logical channels per carrier: 1 FDMA, 2 Phase 2 TDMA
}

// RFSSStatus identifies the site we are listening to.
type RFSSStatus struct {
	SystemID     uint16
	RFSS         uint8
	Site         uint8
	Channel      Channel
	ServiceClass uint8
}

// NetworkStatus identifies the wide-area network.
type NetworkStatus struct {
	WACN         uint32
	SystemID     uint16
	Channel      Channel
	ServiceClass uint8
}

// AdjacentSite advertises a neighbouring site's control channel.
type AdjacentSite struct {
	SystemID     uint16
	RFSS         uint8
	Site         uint8
	Channel      Channel
	ServiceClass uint8
}

// SecondaryControl lists this site's alternate control channels.
type SecondaryControl struct {
	RFSS     uint8
	Site     uint8
	Channels []Channel
}

// PatchUpdate (Motorola) adds talkgroups to, or removes them from, a supergroup.
type PatchUpdate struct {
	SuperGroup uint16
	Groups     []uint16
	Add        bool
}

func (GroupVoiceGrant) isMessage()    {}
func (UnitVoiceGrant) isMessage()     {}
func (GroupAffiliation) isMessage()   {}
func (UnitRegistration) isMessage()   {}
func (UnitDeregistration) isMessage() {}
func (IdentifierUpdate) isMessage()   {}
func (RFSSStatus) isMessage()         {}
func (NetworkStatus) isMessage()      {}
func (AdjacentSite) isMessage()       {}
func (SecondaryControl) isMessage()   {}
func (PatchUpdate) isMessage()        {}

// slotsPerCarrier by IDEN_UP_TDMA channel type (TIA-102.BBAC).
var slotsPerCarrier = [16]int{1, 1, 1, 2, 4, 2}

// Messages decodes a TSBK into zero or more messages. Opcodes we don't
// model yet decode to nothing.
func (t TSBK) Messages() []Message {
	f := t.field
	switch t.MFID {
	case 0x00:
		switch t.Opcode {
		case 0x00: // GRP_V_CH_GRANT
			return []Message{GroupVoiceGrant{
				Options: ServiceOptions(f(0, 8)), Channel: Channel(f(8, 16)),
				Group: uint16(f(24, 16)), Source: uint32(f(40, 24)),
			}}
		case 0x02: // GRP_V_CH_GRANT_UPDT: two channel/group pairs
			out := []Message{GroupVoiceGrant{Channel: Channel(f(0, 16)), Group: uint16(f(16, 16)), Update: true}}
			if b := (GroupVoiceGrant{Channel: Channel(f(32, 16)), Group: uint16(f(48, 16)), Update: true}); b.Group != out[0].(GroupVoiceGrant).Group {
				out = append(out, b)
			}
			return out
		case 0x03: // GRP_V_CH_GRANT_UPDT_EXP
			return []Message{GroupVoiceGrant{
				Options: ServiceOptions(f(0, 8)), Channel: Channel(f(16, 16)),
				Group: uint16(f(48, 16)), Update: true,
			}}
		case 0x04, 0x06: // UU_V_CH_GRANT / UU_V_CH_GRANT_UPDT
			return []Message{UnitVoiceGrant{
				Channel: Channel(f(0, 16)), Target: uint32(f(16, 24)),
				Source: uint32(f(40, 24)), Update: t.Opcode == 0x06,
			}}
		case 0x28: // GRP_AFF_RSP
			return []Message{GroupAffiliation{
				Result: uint8(f(6, 2)), AnnouncementGroup: uint16(f(8, 16)),
				Group: uint16(f(24, 16)), Target: uint32(f(40, 24)),
			}}
		case 0x2C: // U_REG_RSP
			return []Message{UnitRegistration{
				Result: uint8(f(2, 2)), SystemID: uint16(f(4, 12)),
				SourceID: uint32(f(16, 24)), Source: uint32(f(40, 24)),
			}}
		case 0x2F: // U_DE_REG_ACK
			return []Message{UnitDeregistration{
				WACN: uint32(f(8, 20)), SystemID: uint16(f(28, 12)), Source: uint32(f(40, 24)),
			}}
		case 0x33: // IDEN_UP_TDMA
			ct := f(4, 4)
			return []Message{IdentifierUpdate{
				ID: uint8(f(0, 4)), TDMA: ct >= 2 && slotsPerCarrier[ct] > 1, Slots: max(1, slotsPerCarrier[ct]),
				TxOffset: signedOffset(f(8, 14), 14), SpacingHz: f(22, 10) * 125,
				BaseHz: f(32, 32) * 5, Bandwidth: 12_500,
			}}
		case 0x34: // IDEN_UP_VU
			bw := map[uint64]uint64{4: 6_250, 5: 12_500}[f(4, 4)]
			return []Message{IdentifierUpdate{
				ID: uint8(f(0, 4)), Bandwidth: bw, Slots: 1,
				TxOffset: signedOffset(f(8, 14), 14), SpacingHz: f(22, 10) * 125,
				BaseHz: f(32, 32) * 5,
			}}
		case 0x3D: // IDEN_UP
			return []Message{IdentifierUpdate{
				ID: uint8(f(0, 4)), Bandwidth: f(4, 9) * 125, Slots: 1,
				TxOffset: signedOffset(f(13, 9), 9), SpacingHz: f(22, 10) * 125,
				BaseHz: f(32, 32) * 5,
			}}
		case 0x39: // SCCB
			out := SecondaryControl{RFSS: uint8(f(0, 8)), Site: uint8(f(8, 8)), Channels: []Channel{Channel(f(16, 16))}}
			if c2 := Channel(f(40, 16)); c2 != out.Channels[0] {
				out.Channels = append(out.Channels, c2)
			}
			return []Message{out}
		case 0x3A: // RFSS_STS_BCST
			return []Message{RFSSStatus{
				SystemID: uint16(f(12, 12)), RFSS: uint8(f(24, 8)), Site: uint8(f(32, 8)),
				Channel: Channel(f(40, 16)), ServiceClass: uint8(f(56, 8)),
			}}
		case 0x3B: // NET_STS_BCST
			return []Message{NetworkStatus{
				WACN: uint32(f(8, 20)), SystemID: uint16(f(28, 12)),
				Channel: Channel(f(40, 16)), ServiceClass: uint8(f(56, 8)),
			}}
		case 0x3C: // ADJ_STS_BCST
			return []Message{AdjacentSite{
				SystemID: uint16(f(12, 12)), RFSS: uint8(f(24, 8)), Site: uint8(f(32, 8)),
				Channel: Channel(f(40, 16)), ServiceClass: uint8(f(56, 8)),
			}}
		}
	case 0x90: // Motorola
		switch t.Opcode {
		case 0x00, 0x01: // MOT_GRG_ADD_CMD / MOT_GRG_DEL_CMD
			p := PatchUpdate{SuperGroup: uint16(f(0, 16)), Add: t.Opcode == 0x00}
			for _, from := range []uint{16, 32, 48} {
				if g := uint16(f(from, 16)); g != p.SuperGroup && !containsU16(p.Groups, g) {
					p.Groups = append(p.Groups, g)
				}
			}
			return []Message{p}
		case 0x02: // MOT_GRG_CH_GRANT
			return []Message{GroupVoiceGrant{
				Options: ServiceOptions(f(0, 8)), Channel: Channel(f(8, 16)),
				Group: uint16(f(24, 16)), Source: uint32(f(40, 24)), Patch: true,
			}}
		case 0x03: // MOT_GRG_CH_GRANT_UPDT
			out := []Message{GroupVoiceGrant{Channel: Channel(f(0, 16)), Group: uint16(f(16, 16)), Update: true, Patch: true}}
			if b := (GroupVoiceGrant{Channel: Channel(f(32, 16)), Group: uint16(f(48, 16)), Update: true, Patch: true}); b.Group != out[0].(GroupVoiceGrant).Group {
				out = append(out, b)
			}
			return out
		}
	}
	return nil
}

// signedOffset decodes a sign-magnitude transmit offset in 250 kHz steps.
func signedOffset(v uint64, n uint) int64 {
	mag := int64(v&(1<<(n-1)-1)) * 250_000
	if v>>(n-1) == 0 {
		return -mag // sign bit 0 = negative (inbound below outbound)
	}
	return mag
}

func containsU16(s []uint16, v uint16) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// ChannelPlan resolves Channels to frequencies using the IdentifierUpdates
// the control channel broadcasts.
type ChannelPlan struct {
	idens [16]*IdentifierUpdate
}

func (p *ChannelPlan) Update(u IdentifierUpdate) { p.idens[u.ID] = &u }

// Resolve returns the downlink frequency and TDMA slot (0 for FDMA) of c, or
// ok=false if its identifier hasn't been broadcast yet.
func (p *ChannelPlan) Resolve(c Channel) (hz uint64, slot int, ok bool) {
	u := p.idens[c.Iden()]
	if u == nil {
		return 0, 0, false
	}
	n := uint64(c.Number())
	slots := uint64(max(u.Slots, 1))
	return u.BaseHz + u.SpacingHz*(n/slots), int(n % slots), true
}

// Entries returns the known channel-plan entries in identifier order.
func (p *ChannelPlan) Entries() []PlanEntry {
	var out []PlanEntry
	for _, u := range p.idens {
		if u != nil {
			out = append(out, PlanEntry{
				ID: u.ID, BaseHz: u.BaseHz, SpacingHz: u.SpacingHz,
				TxOffset: u.TxOffset, Bandwidth: u.Bandwidth, Slots: u.Slots,
			})
		}
	}
	return out
}
