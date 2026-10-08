package p25

import "time"

// Call is one transmission on a talkgroup or unit-to-unit voice channel, as
// seen from the control channel: a grant plus however long grant updates
// keep repeating it.
type Call struct {
	Group     uint16 // 0 for unit-to-unit calls
	Target    uint32 // unit-to-unit only
	Source    uint32 // radio ID that keyed up; 0 if we only saw updates
	Channel   Channel
	FreqHz    uint64 // 0 if the channel plan wasn't known yet
	Slot      int
	Encrypted bool
	Emergency bool
	Patch     bool
	Start     time.Time
	LastSeen  time.Time
}

// callIdle is how long without a grant or update before a call is over.
// Motorola repeats updates every second or so while a channel is up.
const callIdle = 3 * time.Second

type callKey struct {
	group   uint16
	target  uint32
	channel Channel
}

// CallTracker folds the grant/update stream into calls. Feed it messages in
// order with the time they were received; it reports each call once when it
// starts and once when it ends.
type CallTracker struct {
	Plan    ChannelPlan
	OnStart func(Call)
	OnEnd   func(Call)

	active map[callKey]*Call
}

func (t *CallTracker) Handle(m Message, now time.Time) {
	if t.active == nil {
		t.active = map[callKey]*Call{}
	}
	switch v := m.(type) {
	case IdentifierUpdate:
		t.Plan.Update(v)
	case GroupVoiceGrant:
		t.touch(callKey{group: v.Group, channel: v.Channel}, v.Source, v.Options, !v.Update, v.Patch, now)
	case UnitVoiceGrant:
		t.touch(callKey{target: v.Target, channel: v.Channel}, v.Source, v.Options, !v.Update, false, now)
	}
	t.Expire(now)
}

func (t *CallTracker) touch(k callKey, src uint32, opts ServiceOptions, initial, patch bool, now time.Time) {
	if c, ok := t.active[k]; ok && (!initial || src == 0 || c.Source == 0 || c.Source == src) {
		c.LastSeen = now
		if src != 0 {
			c.Source = src
		}
		if c.FreqHz == 0 {
			// Joined mid-call before the channel plan had been broadcast.
			c.FreqHz, c.Slot, _ = t.Plan.Resolve(k.channel)
		}
		// Updates carry no service options; only an explicit grant can
		// tell us a call is encrypted.
		if initial {
			c.Encrypted = c.Encrypted || opts.Encrypted()
			c.Emergency = c.Emergency || opts.Emergency()
		}
		return
	}
	// A different call on the same channel means the old one is over. That
	// includes a new talker on the same talkgroup: Motorola holds the voice
	// channel between transmissions and grants each new keyup onto it, and
	// we track calls per transmission.
	for ak, c := range t.active {
		if ak.channel == k.channel {
			t.end(ak, c)
		}
	}
	hz, slot, _ := t.Plan.Resolve(k.channel)
	c := &Call{
		Group: k.group, Target: k.target, Source: src, Channel: k.channel,
		FreqHz: hz, Slot: slot, Encrypted: opts.Encrypted(), Emergency: opts.Emergency(),
		Patch: patch, Start: now, LastSeen: now,
	}
	t.active[k] = c
	if t.OnStart != nil {
		t.OnStart(*c)
	}
}

// Expire ends calls that have gone quiet. Handle calls it; call it on a
// timer too so calls end while the control channel is idle.
func (t *CallTracker) Expire(now time.Time) {
	for k, c := range t.active {
		if now.Sub(c.LastSeen) > callIdle {
			t.end(k, c)
		}
	}
}

func (t *CallTracker) end(k callKey, c *Call) {
	delete(t.active, k)
	if t.OnEnd != nil {
		t.OnEnd(*c)
	}
}
