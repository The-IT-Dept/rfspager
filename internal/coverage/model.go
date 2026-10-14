// Package coverage builds the admin coverage map: which paging transmitters
// our receivers can hear, which RFS districts' pages reach us, and where the
// incidents behind them were. POCSAG does not identify the transmitter, so
// the transmitter a receiver hears is inferred from the districts it heard
// and a simple link model. Design in docs/plans/2026-10-14-coverage-map.md.
package coverage

import (
	"math"
	"sort"
)

// Link model parameters (see "Probable transmitter model" in the plan).
const (
	// defaultEIRPW stands in for transmitters ACMA lists without an EIRP.
	defaultEIRPW = 50.0
	// defaultHeightM stands in for a missing antenna height above ground.
	defaultHeightM = 10.0
	// receiverHeightM is the assumed receiver antenna height for the radio
	// horizon. Receivers sit on roofs and hills; we do not know which.
	receiverHeightM = 30.0
	// pathLossDBPerDecade is the distance exponent: 35 dB per decade, between
	// free space (20) and cluttered terrain (40).
	pathLossDBPerDecade = 35.0
	// beyondHorizonDB penalises transmitters past horizonSlack x the radio
	// horizon. Not impossible (ducting, diffraction), just unlikely.
	beyondHorizonDB = 20.0
	horizonSlack    = 1.3
	// possibleWithinDB and maxPossible bound the runner-up transmitters shown
	// as "possible" next to the probable one.
	possibleWithinDB = 6.0
	maxPossible      = 3
	// minLinkPages is how many distinct pages to known capcodes a receiver
	// must have heard from a district before we draw a link for it, so a
	// stray decode that happens to hit a real capcode does not count.
	minLinkPages = 3
)

// Transmitter statuses, in precedence order.
const (
	StatusProbable      = "probable"
	StatusPossible      = "possible"
	StatusDistrictHeard = "district_heard"
	StatusNotHeard      = "not_heard"
	StatusUnmapped      = "unmapped"
)

// Channels.
const (
	ChannelRFS   = "RFS"
	ChannelFRNSW = "FRNSW"
	ChannelAll   = "all"
)

const earthRadiusKm = 6371.0088

// distanceKm is the great-circle distance between two points.
func distanceKm(lat1, lng1, lat2, lng2 float64) float64 {
	const rad = math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLng := (lng2 - lng1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * earthRadiusKm * math.Asin(math.Min(1, math.Sqrt(a)))
}

// radioHorizonKm is the 4/3-earth radio horizon between the transmitter
// antenna (ground elevation plus mast height) and a receiver at
// receiverHeightM.
func radioHorizonKm(t *Transmitter) float64 {
	h := deref(t.ElevationM, 0) + deref(t.HeightM, defaultHeightM)
	if h < 0 {
		h = 0
	}
	return 4.12 * (math.Sqrt(h) + math.Sqrt(receiverHeightM))
}

// linkScoreDB is a relative received level for t heard at distKm. Only
// differences between transmitters matter.
func linkScoreDB(t *Transmitter, distKm float64) float64 {
	eirp := deref(t.EIRPW, defaultEIRPW)
	if eirp <= 0 {
		eirp = defaultEIRPW
	}
	score := 10*math.Log10(eirp) - pathLossDBPerDecade*math.Log10(math.Max(distKm, 1))
	if distKm > horizonSlack*radioHorizonKm(t) {
		score -= beyondHorizonDB
	}
	return score
}

// candidate is one transmitter scored for one receiver.
type candidate struct {
	T          *Transmitter
	DistanceKm float64
	ScoreDB    float64
}

// rankTransmitters picks the transmitter of one district a receiver at
// (lat, lng) most likely heard, plus up to maxPossible others within
// possibleWithinDB of it. ok is false when txs is empty.
func rankTransmitters(lat, lng float64, txs []*Transmitter) (probable candidate, possible []candidate, ok bool) {
	if len(txs) == 0 {
		return candidate{}, nil, false
	}
	cands := make([]candidate, 0, len(txs))
	for _, t := range txs {
		d := distanceKm(lat, lng, t.Lat, t.Lng)
		cands = append(cands, candidate{T: t, DistanceKm: d, ScoreDB: linkScoreDB(t, d)})
	}
	// Ties go to the nearer transmitter, then the lower id, so the answer is
	// stable between requests.
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].ScoreDB != cands[j].ScoreDB {
			return cands[i].ScoreDB > cands[j].ScoreDB
		}
		if cands[i].DistanceKm != cands[j].DistanceKm {
			return cands[i].DistanceKm < cands[j].DistanceKm
		}
		return cands[i].T.ID < cands[j].T.ID
	})
	probable = cands[0]
	for _, c := range cands[1:] {
		if len(possible) == maxPossible || c.ScoreDB < probable.ScoreDB-possibleWithinDB {
			break
		}
		possible = append(possible, c)
	}
	return probable, possible, true
}

func deref(p *float64, def float64) float64 {
	if p == nil {
		return def
	}
	return *p
}

// round rounds to n decimal places.
func round(v float64, n int) float64 {
	p := math.Pow(10, float64(n))
	return math.Round(v*p) / p
}
