package coverage

import (
	"sort"
	"strings"
	"time"
)

// Params selects a coverage report. Normalise with Normalize before use; the
// normalised value is also the cache key.
type Params struct {
	Days     int
	SystemID int64 // 0 = every receiver
	Channel  string
}

// Bounds on Params.Days.
const (
	DefaultDays = 30
	MaxDays     = 365
)

// Normalize clamps Days into 1..MaxDays (0 means the default) and maps an
// unknown channel to RFS. It reports whether the input was valid.
func (p Params) Normalize() (Params, bool) {
	ok := true
	if p.Days == 0 {
		p.Days = DefaultDays
	}
	if p.Days < 1 || p.Days > MaxDays {
		ok = false
		p.Days = min(max(p.Days, 1), MaxDays)
	}
	switch strings.ToUpper(p.Channel) {
	case "", ChannelRFS:
		p.Channel = ChannelRFS
	case ChannelFRNSW:
		p.Channel = ChannelFRNSW
	case "ALL":
		p.Channel = ChannelAll
	default:
		ok = false
		p.Channel = ChannelRFS
	}
	if p.SystemID < 0 {
		ok = false
		p.SystemID = 0
	}
	return p, ok
}

func (p Params) includes(channel string) bool {
	return p.Channel == ChannelAll || p.Channel == channel
}

// Response is GET /api/v1/admin/coverage. The JSON shape is the contract
// with the admin UI (docs/plans/2026-10-14-coverage-map.md).
type Response struct {
	Window          Window     `json:"window"`
	ReceptionsSince *time.Time `json:"receptions_since"`
	GeneratedAt     time.Time  `json:"generated_at"`
	// TotalPages is every distinct page (address + minute) in scope,
	// whether or not it resolved to a district.
	TotalPages       int              `json:"total_pages"`
	Receivers        []Receiver       `json:"receivers"`
	Transmitters     []TransmitterOut `json:"transmitters"`
	Links            []Link           `json:"links"`
	Districts        []District       `json:"districts"`
	UnmappedPrefixes []PrefixPages    `json:"unmapped_prefixes"`
	// Heat is [lng, lat, weight] per ~1 km cell.
	Heat [][3]float64 `json:"heat"`
}

type Window struct {
	Days  int       `json:"days"`
	Since time.Time `json:"since"`
	Until time.Time `json:"until"`
}

type Receiver struct {
	ID             int64      `json:"id"`
	Name           string     `json:"name"`
	Lat            float64    `json:"lat"`
	Lng            float64    `json:"lng"`
	LocatedSince   *time.Time `json:"located_since"`
	Pages          int        `json:"pages"`
	DistrictsHeard int        `json:"districts_heard"`
	ReachKm        float64    `json:"reach_km"`
	LastHeardAt    *time.Time `json:"last_heard_at"`
}

type TransmitterOut struct {
	ID                 int64    `json:"id"`
	Channel            string   `json:"channel"`
	Name               string   `json:"name"`
	Licensee           string   `json:"licensee"`
	Lat                float64  `json:"lat"`
	Lng                float64  `json:"lng"`
	EIRPW              *float64 `json:"eirp_w"`
	HeightM            *float64 `json:"height_m"`
	ElevationM         *float64 `json:"elevation_m"`
	AgencyID           *int64   `json:"agency_id"`
	Agency             *string  `json:"agency"`
	DistrictSource     string   `json:"district_source"`
	DistrictConfidence *float64 `json:"district_confidence"`
	Status             string   `json:"status"`
	HeardBy            []int64  `json:"heard_by"`
	Pages              int      `json:"pages"`
}

type Link struct {
	SystemID      int64   `json:"system_id"`
	TransmitterID int64   `json:"transmitter_id"`
	Kind          string  `json:"kind"` // probable | possible
	AgencyID      int64   `json:"agency_id"`
	Pages         int     `json:"pages"`
	DistanceKm    float64 `json:"distance_km"`
	ScoreDB       float64 `json:"score_db"`
}

type District struct {
	AgencyID int64  `json:"agency_id"`
	Agency   string `json:"agency"`
	Channel  string `json:"channel"`
	Pages    int    `json:"pages"`
	// UnknownCapcodePages are pages to addresses with no capcode that the
	// prefix map placed in this district. Kept apart from Pages because
	// garbled noise decodes land here too, so they never drive links or
	// transmitter status.
	UnknownCapcodePages int             `json:"unknown_capcode_pages"`
	Receivers           []DistrictHeard `json:"receivers"`
	CapcodesHeard       int             `json:"capcodes_heard"`
	CapcodesKnown       int             `json:"capcodes_known"`
	Transmitters        int             `json:"transmitters"`
	LastHeardAt         *time.Time      `json:"last_heard_at"`
}

type DistrictHeard struct {
	SystemID int64 `json:"system_id"`
	Pages    int   `json:"pages"`
}

type PrefixPages struct {
	Prefix string `json:"prefix"`
	Pages  int    `json:"pages"`
}

// Agency is an agencies row.
type Agency struct {
	ID               int64  `db:"id"`
	Name             string `db:"agency"`
	CoverageDistrict bool   `db:"coverage_district"`
}

// ReceiverSite is a located, approved, non-deleted system.
type ReceiverSite struct {
	ID           int64      `db:"id"`
	Name         string     `db:"name"`
	Lat          float64    `db:"lat"`
	Lng          float64    `db:"lng"`
	LocatedSince *time.Time `db:"located_since"`
}

// Transmitter is a paging_transmitters row.
type Transmitter struct {
	ID                 int64    `db:"id"`
	Channel            string   `db:"channel"`
	Name               string   `db:"name"`
	Licensee           string   `db:"licensee"`
	Lat                float64  `db:"lat"`
	Lng                float64  `db:"lng"`
	EIRPW              *float64 `db:"eirp_w"`
	HeightM            *float64 `db:"height_m"`
	ElevationM         *float64 `db:"elevation_m"`
	AgencyID           *int64   `db:"agency_id"`
	DistrictSource     string   `db:"district_source"`
	DistrictConfidence *float64 `db:"district_confidence"`
}

// systemAddressPages is the window's receptions of one address by one
// system, split by whether they happened at the system's current location.
// Pages counts distinct minutes, so a re-send within the minute counts once.
type systemAddressPages struct {
	SystemID     int64     `db:"system_id"`
	Address      string    `db:"address"`
	CapcodeID    *int64    `db:"capcode_id"`
	Attributable bool      `db:"attributable"`
	Pages        int       `db:"pages"`
	LastHeard    time.Time `db:"last_heard"`
}

// addressPages is the window's receptions of one address across every
// system in scope, as distinct minutes: the same page heard by several
// receivers counts once.
type addressPages struct {
	Address   string `db:"address"`
	CapcodeID *int64 `db:"capcode_id"`
	Pages     int    `db:"pages"`
}

// reportInputs is everything build needs, loaded by Service.
type reportInputs struct {
	params          Params
	since, until    time.Time
	agencies        []Agency
	capcodes        []Capcode
	receivers       []ReceiverSite
	transmitters    []Transmitter
	systemPages     []systemAddressPages
	addressPages    []addressPages
	heat            [][3]float64
	lastHeard       map[int64]time.Time
	receptionsSince *time.Time
}

// agencyIndex answers the per-agency questions build and the loaders share.
type agencyIndex struct {
	byID    map[int64]Agency
	frnswID int64
}

func newAgencyIndex(agencies []Agency) agencyIndex {
	ix := agencyIndex{byID: make(map[int64]Agency, len(agencies))}
	for _, a := range agencies {
		ix.byID[a.ID] = a
		if isFRNSWAgency(a.Name) {
			ix.frnswID = a.ID
		}
	}
	return ix
}

// isFRNSWAgency recognises the FRNSW agency, whose pages go out on the
// FRNSW channel. Every other agency pages on the RFS channel.
func isFRNSWAgency(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), "FRNSW")
}

// channelOf is the paging channel an agency's pages (and an unresolved
// address's pages, agencyID 0) go out on.
func (ix agencyIndex) channelOf(agencyID int64) string {
	if agencyID != 0 && agencyID == ix.frnswID {
		return ChannelFRNSW
	}
	return ChannelRFS
}

// listed reports whether an agency gets a row in the district table: the
// RFS districts, plus FRNSW.
func (ix agencyIndex) listed(agencyID int64) bool {
	a, ok := ix.byID[agencyID]
	return ok && (a.CoverageDistrict || a.ID == ix.frnswID)
}

// build assembles the report from loaded inputs. Pure, so the model and the
// bookkeeping are unit-testable without a database.
func build(in reportInputs, now time.Time) *Response {
	p := in.params
	ix := newAgencyIndex(in.agencies)
	resolver := NewResolver(in.capcodes)

	// District totals across every receiver in scope, located or not. Only
	// pages to known capcodes count as hearing a district: an unknown
	// address is as likely a garbled decode as a real capcode we lack.
	totalPages := 0
	districtPages := map[int64]int{}
	districtPrefixPages := map[int64]int{}
	capcodesHeard := map[int64]map[int64]bool{}
	unmapped := map[string]int{}
	for _, ap := range in.addressPages {
		res := resolver.Resolve(ap.Address, derefID(ap.CapcodeID))
		if !p.includes(ix.channelOf(res.AgencyID)) {
			continue
		}
		totalPages += ap.Pages
		if res.AgencyID == 0 {
			unmapped[addressPrefix(ap.Address)] += ap.Pages
			continue
		}
		if res.CapcodeID == 0 {
			districtPrefixPages[res.AgencyID] += ap.Pages
			continue
		}
		districtPages[res.AgencyID] += ap.Pages
		if capcodesHeard[res.AgencyID] == nil {
			capcodesHeard[res.AgencyID] = map[int64]bool{}
		}
		capcodesHeard[res.AgencyID][res.CapcodeID] = true
	}

	// Per-receiver attribution: located receivers, receptions at their
	// current location only.
	located := make(map[int64]ReceiverSite, len(in.receivers))
	for _, r := range in.receivers {
		located[r.ID] = r
	}
	recvPages := map[int64]int{}
	recvDistrict := map[int64]map[int64]int{}
	recvLast := map[int64]time.Time{}
	for _, sp := range in.systemPages {
		if _, ok := located[sp.SystemID]; !ok {
			continue
		}
		res := resolver.Resolve(sp.Address, derefID(sp.CapcodeID))
		if !p.includes(ix.channelOf(res.AgencyID)) {
			continue
		}
		if sp.LastHeard.After(recvLast[sp.SystemID]) {
			recvLast[sp.SystemID] = sp.LastHeard
		}
		if !sp.Attributable {
			continue
		}
		recvPages[sp.SystemID] += sp.Pages
		if res.CapcodeID != 0 && res.AgencyID != 0 && ix.listed(res.AgencyID) {
			if recvDistrict[sp.SystemID] == nil {
				recvDistrict[sp.SystemID] = map[int64]int{}
			}
			recvDistrict[sp.SystemID][res.AgencyID] += sp.Pages
		}
	}

	// Transmitters in scope, grouped by district.
	txByAgency := map[int64][]*Transmitter{}
	txCount := map[int64]int{}
	var txs []*Transmitter
	for i := range in.transmitters {
		t := &in.transmitters[i]
		if !p.includes(t.Channel) {
			continue
		}
		txs = append(txs, t)
		if t.AgencyID != nil {
			txCount[*t.AgencyID]++
			if t.Channel == ix.channelOf(*t.AgencyID) {
				txByAgency[*t.AgencyID] = append(txByAgency[*t.AgencyID], t)
			}
		}
	}

	// The link model: for each receiver and each district it heard at least
	// minLinkPages pages of, the district's best transmitter is probable and
	// close runners-up are possible.
	links := []Link{}
	reach := map[int64]float64{}
	for _, r := range in.receivers {
		districts := recvDistrict[r.ID]
		ids := make([]int64, 0, len(districts))
		for id := range districts {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, agencyID := range ids {
			pages := districts[agencyID]
			if pages < minLinkPages {
				continue
			}
			best, others, ok := rankTransmitters(r.Lat, r.Lng, txByAgency[agencyID])
			if !ok {
				continue
			}
			links = append(links, newLink(r.ID, agencyID, pages, StatusProbable, best))
			reach[r.ID] = max(reach[r.ID], best.DistanceKm)
			for _, c := range others {
				links = append(links, newLink(r.ID, agencyID, pages, StatusPossible, c))
			}
		}
	}

	// Transmitter status from the links, then from district activity.
	kind := map[int64]string{}
	heardBy := map[int64][]int64{}
	txPages := map[int64]int{}
	for _, l := range links {
		if kind[l.TransmitterID] != StatusProbable {
			kind[l.TransmitterID] = l.Kind
		}
		heardBy[l.TransmitterID] = append(heardBy[l.TransmitterID], l.SystemID)
		txPages[l.TransmitterID] += l.Pages
	}
	outTx := make([]TransmitterOut, 0, len(txs))
	for _, t := range txs {
		o := TransmitterOut{
			ID: t.ID, Channel: t.Channel, Name: t.Name, Licensee: t.Licensee,
			Lat: t.Lat, Lng: t.Lng, EIRPW: t.EIRPW, HeightM: t.HeightM, ElevationM: t.ElevationM,
			AgencyID: t.AgencyID, DistrictSource: t.DistrictSource, DistrictConfidence: t.DistrictConfidence,
			HeardBy: heardBy[t.ID], Pages: txPages[t.ID],
		}
		if o.HeardBy == nil {
			o.HeardBy = []int64{}
		} else {
			o.HeardBy = uniqueSorted(o.HeardBy)
		}
		switch {
		case kind[t.ID] != "":
			o.Status = kind[t.ID]
		case t.AgencyID == nil:
			o.Status = StatusUnmapped
		case districtPages[*t.AgencyID] > 0:
			o.Status = StatusDistrictHeard
		default:
			o.Status = StatusNotHeard
		}
		if t.AgencyID != nil {
			if a, ok := ix.byID[*t.AgencyID]; ok {
				name := a.Name
				o.Agency = &name
			}
		}
		outTx = append(outTx, o)
	}

	receivers := make([]Receiver, 0, len(in.receivers))
	for _, r := range in.receivers {
		out := Receiver{
			ID: r.ID, Name: r.Name, Lat: r.Lat, Lng: r.Lng, LocatedSince: r.LocatedSince,
			Pages: recvPages[r.ID], DistrictsHeard: len(recvDistrict[r.ID]),
			ReachKm: round(reach[r.ID], 1),
		}
		if t, ok := recvLast[r.ID]; ok {
			t := t.UTC()
			out.LastHeardAt = &t
		}
		receivers = append(receivers, out)
	}

	districts := []District{}
	for _, a := range in.agencies {
		if !ix.listed(a.ID) || !p.includes(ix.channelOf(a.ID)) {
			continue
		}
		d := District{
			AgencyID: a.ID, Agency: a.Name, Channel: ix.channelOf(a.ID),
			Pages: districtPages[a.ID], UnknownCapcodePages: districtPrefixPages[a.ID],
			Receivers:     []DistrictHeard{},
			CapcodesHeard: len(capcodesHeard[a.ID]), CapcodesKnown: 0,
			Transmitters: txCount[a.ID],
		}
		for _, r := range in.receivers {
			if n := recvDistrict[r.ID][a.ID]; n > 0 {
				d.Receivers = append(d.Receivers, DistrictHeard{SystemID: r.ID, Pages: n})
			}
		}
		sort.SliceStable(d.Receivers, func(i, j int) bool { return d.Receivers[i].Pages > d.Receivers[j].Pages })
		if t, ok := in.lastHeard[a.ID]; ok {
			t := t.UTC()
			d.LastHeardAt = &t
		}
		districts = append(districts, d)
	}
	known := knownCapcodes(in.capcodes)
	for i := range districts {
		districts[i].CapcodesKnown = known[districts[i].AgencyID]
	}
	sort.SliceStable(districts, func(i, j int) bool {
		if districts[i].Pages != districts[j].Pages {
			return districts[i].Pages > districts[j].Pages
		}
		return districts[i].Agency < districts[j].Agency
	})

	prefixes := make([]PrefixPages, 0, len(unmapped))
	for prefix, n := range unmapped {
		prefixes = append(prefixes, PrefixPages{Prefix: prefix, Pages: n})
	}
	sort.Slice(prefixes, func(i, j int) bool {
		if prefixes[i].Pages != prefixes[j].Pages {
			return prefixes[i].Pages > prefixes[j].Pages
		}
		return prefixes[i].Prefix < prefixes[j].Prefix
	})

	heat := in.heat
	if heat == nil {
		heat = [][3]float64{}
	}
	return &Response{
		Window:           Window{Days: p.Days, Since: in.since.UTC(), Until: in.until.UTC()},
		ReceptionsSince:  in.receptionsSince,
		GeneratedAt:      now.UTC(),
		TotalPages:       totalPages,
		Receivers:        receivers,
		Transmitters:     outTx,
		Links:            links,
		Districts:        districts,
		UnmappedPrefixes: prefixes,
		Heat:             heat,
	}
}

func newLink(systemID, agencyID int64, pages int, kind string, c candidate) Link {
	return Link{
		SystemID: systemID, TransmitterID: c.T.ID, Kind: kind, AgencyID: agencyID,
		Pages: pages, DistanceKm: round(c.DistanceKm, 1), ScoreDB: round(c.ScoreDB, 1),
	}
}

// knownCapcodes counts each agency's live, non-ignored capcodes.
func knownCapcodes(capcodes []Capcode) map[int64]int {
	out := map[int64]int{}
	for _, c := range capcodes {
		if c.AgencyID != nil && !c.Ignore {
			out[*c.AgencyID]++
		}
	}
	return out
}

// addressPrefix is the 4-digit prefix an unmapped address is grouped under.
func addressPrefix(address string) string {
	if len(address) < prefixLen {
		return address
	}
	return address[:prefixLen]
}

func derefID(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func uniqueSorted(ids []int64) []int64 {
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := ids[:0]
	for i, id := range ids {
		if i == 0 || id != ids[i-1] {
			out = append(out, id)
		}
	}
	return out
}
