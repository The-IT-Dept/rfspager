package p25

import (
	"crypto/rand"
	"encoding/hex"
	"reflect"
	"sort"
	"time"
)

// Reporter turns the decoded message stream into the wire payloads in
// wire.go: it tracks which site it's on and that site's channel plan and
// neighbours, folds grants into calls, and drops the repeats the network
// sends of every unit registration and affiliation.
type Reporter struct {
	// Send delivers one frame. Called synchronously from Handle and Tick.
	Send func(frameType string, payload any)

	calls     CallTracker
	site      SitePayload
	haveRFSS  bool
	haveNet   bool
	siteFrom  time.Time // when RFSS status was first decoded on this channel
	neighbors map[[2]uint8]NeighborSite
	sentSite  SitePayload
	siteAt    time.Time

	unitSeen map[UnitPayload]time.Time // key has TSMS zeroed
	callIDs  map[Call]string           // keyed by the call as first reported
}

// unitRepeatWindow: the FNE repeats each unit response a few times within
// a second or two; a genuine re-affiliation is much rarer than this.
const unitRepeatWindow = 10 * time.Second

// siteRefresh re-sends the site payload even when unchanged, so the server
// can show which sites are being heard right now.
const siteRefresh = 5 * time.Minute

// siteSettle: the channel plan and neighbour list arrive over several
// seconds after tuning. Wait for them rather than send a dribble of partial
// site payloads, and after that send changes at most this often.
const siteSettle = 10 * time.Second

func NewReporter(send func(string, any)) *Reporter {
	r := &Reporter{
		Send:      send,
		neighbors: map[[2]uint8]NeighborSite{},
		unitSeen:  map[UnitPayload]time.Time{},
		callIDs:   map[Call]string{},
	}
	r.calls.OnStart = r.callStart
	r.calls.OnEnd = r.callEnd
	return r
}

// Handle processes one decoded message received at now.
func (r *Reporter) Handle(m Message, now time.Time) {
	r.calls.Handle(m, now)
	switch v := m.(type) {
	case RFSSStatus:
		r.site.SysID, r.site.RFSS, r.site.Site = v.SystemID, v.RFSS, v.Site
		r.site.ControlHz, _, _ = r.calls.Plan.Resolve(v.Channel)
		if !r.haveRFSS {
			r.haveRFSS, r.siteFrom = true, now
		}
	case NetworkStatus:
		r.site.WACN = v.WACN
		r.haveNet = true
	case AdjacentSite:
		hz, _, _ := r.calls.Plan.Resolve(v.Channel)
		r.neighbors[[2]uint8{v.RFSS, v.Site}] = NeighborSite{SysID: v.SystemID, RFSS: v.RFSS, Site: v.Site, ControlHz: hz}
	case GroupAffiliation:
		r.unit(UnitPayload{Kind: UnitKindAffiliation, Unit: v.Target, Group: v.Group, Result: v.Result}, now)
	case UnitRegistration:
		r.unit(UnitPayload{Kind: UnitKindRegistration, Unit: v.Source, Result: v.Result}, now)
	case UnitDeregistration:
		r.unit(UnitPayload{Kind: UnitKindDeregistration, Unit: v.Source}, now)
	}
	r.maybeSendSite(now)
}

// Tick ends calls that have gone quiet and refreshes the site. Call it every
// second or so.
func (r *Reporter) Tick(now time.Time) {
	r.calls.Expire(now)
	r.maybeSendSite(now)
	for k, at := range r.unitSeen {
		if now.Sub(at) > unitRepeatWindow {
			delete(r.unitSeen, k)
		}
	}
}

// Reset forgets the current site, e.g. after retuning to another control
// channel. Calls in progress are ended.
func (r *Reporter) Reset(now time.Time) {
	r.calls.Expire(now.Add(time.Hour))
	r.calls.Plan = ChannelPlan{}
	r.site, r.sentSite = SitePayload{}, SitePayload{}
	r.haveRFSS, r.haveNet = false, false
	clear(r.neighbors)
}

// SetNAC records the NAC the framer is seeing, for the site payload.
func (r *Reporter) SetNAC(nac uint16) { r.site.NAC = nac }

func (r *Reporter) maybeSendSite(now time.Time) {
	if !r.haveRFSS || !r.haveNet || r.site.ControlHz == 0 || now.Sub(r.siteFrom) < siteSettle {
		return
	}
	s := r.site
	s.Plan = r.calls.Plan.Entries()
	for _, n := range r.neighbors {
		s.Neighbors = append(s.Neighbors, n)
	}
	sort.Slice(s.Neighbors, func(i, j int) bool {
		a, b := s.Neighbors[i], s.Neighbors[j]
		return a.RFSS < b.RFSS || a.RFSS == b.RFSS && a.Site < b.Site
	})
	if since := now.Sub(r.siteAt); since < siteSettle ||
		reflect.DeepEqual(s, r.sentSite) && since < siteRefresh {
		return
	}
	r.sentSite, r.siteAt = s, now
	r.Send(FrameSite, s)
}

func (r *Reporter) unit(u UnitPayload, now time.Time) {
	u.SysID, u.RFSS, u.Site = r.site.SysID, r.site.RFSS, r.site.Site
	if at, ok := r.unitSeen[u]; ok && now.Sub(at) < unitRepeatWindow {
		return
	}
	r.unitSeen[u] = now
	u.TSMS = now.UnixMilli()
	r.Send(FrameUnit, u)
}

func (r *Reporter) callPayload(c Call, id string) CallPayload {
	return CallPayload{
		CallID: id, SysID: r.site.SysID, RFSS: r.site.RFSS, Site: r.site.Site,
		Group: c.Group, Target: c.Target, Source: c.Source, FreqHz: c.FreqHz, Slot: c.Slot,
		Encrypted: c.Encrypted, Emergency: c.Emergency, Patch: c.Patch,
		StartMS: c.Start.UnixMilli(),
	}
}

func (r *Reporter) callStart(c Call) {
	id := newCallID()
	r.callIDs[callIdentity(c)] = id
	r.Send(FrameCallStart, r.callPayload(c, id))
}

func (r *Reporter) callEnd(c Call) {
	k := callIdentity(c)
	id := r.callIDs[k]
	delete(r.callIDs, k)
	p := r.callPayload(c, id)
	p.EndMS = c.LastSeen.UnixMilli()
	r.Send(FrameCallEnd, p)
}

// callIdentity reduces a call to the fields that don't change while it runs.
func callIdentity(c Call) Call {
	return Call{Group: c.Group, Target: c.Target, Channel: c.Channel, Start: c.Start}
}

func newCallID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
