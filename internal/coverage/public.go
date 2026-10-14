package coverage

import (
	"context"
	"time"
)

// publicCacheTTL is how long a public coverage response is served before it
// is rebuilt from the admin report.
const publicCacheTTL = 15 * time.Minute

// Public window choices: anything else asked for is served as the default.
const (
	PublicDefaultDays = 90
	publicShortDays   = 30
)

// PublicDays maps a requested window to the ones the public map offers.
func PublicDays(days int) int {
	if days == publicShortDays {
		return publicShortDays
	}
	return PublicDefaultDays
}

// PublicResponse is GET /api/public/coverage: the admin report reduced to
// what is safe and useful in public. No ids, owners, licensees, EIRP, links
// or model scores. The JSON shape is the contract with the marketing site
// (docs/plans/2026-10-14-public-coverage-and-hosting.md).
type PublicResponse struct {
	Window       Window              `json:"window"`
	GeneratedAt  time.Time           `json:"generated_at"`
	TotalPages   int                 `json:"total_pages"`
	Receivers    []PublicReceiver    `json:"receivers"`
	Transmitters []PublicTransmitter `json:"transmitters"`
	Districts    []PublicDistrict    `json:"districts"`
	// Heat is [lng, lat, weight] per ~1 km cell, as in the admin report.
	Heat [][3]float64 `json:"heat"`
}

// PublicReceiver is an approved, located receiver: its name and a position
// rounded to 2 decimals (~1 km), whatever precision is stored.
type PublicReceiver struct {
	Name        string     `json:"name"`
	Lat         float64    `json:"lat"`
	Lng         float64    `json:"lng"`
	LastHeardAt *time.Time `json:"last_heard_at"`
}

// PublicTransmitter is an RFS paging site (public ACMA data). Heard means a
// receiver probably or possibly hears it, or its district was heard.
type PublicTransmitter struct {
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
	Heard    bool    `json:"heard"`
	District *string `json:"district"`
}

// PublicDistrict is an RFS district; heard = any page in the window.
type PublicDistrict struct {
	Name        string     `json:"name"`
	Heard       bool       `json:"heard"`
	Pages       int        `json:"pages"`
	LastHeardAt *time.Time `json:"last_heard_at"`
}

// PublicReport returns the public coverage for a window of PublicDays(days),
// from its own cache when one was built in the last publicCacheTTL. Misses
// are built one at a time so a burst of visitors at expiry builds once.
func (s *Service) PublicReport(ctx context.Context, days int) (*PublicResponse, error) {
	days = PublicDays(days)
	if r, ok := s.public.get(days, s.now()); ok {
		return r, nil
	}
	s.publicMu.Lock()
	defer s.publicMu.Unlock()
	if r, ok := s.public.get(days, s.now()); ok {
		return r, nil
	}
	p, _ := Params{Days: days, Channel: ChannelRFS}.Normalize()
	report, err := s.Report(ctx, p)
	if err != nil {
		return nil, err
	}
	out := Publicize(report)
	s.public.put(days, out, s.now())
	return out, nil
}

// Publicize reduces an RFS-channel admin report to the public response. It
// does not modify r, which may be shared through the report cache.
func Publicize(r *Response) *PublicResponse {
	out := &PublicResponse{
		Window:       r.Window,
		GeneratedAt:  r.GeneratedAt,
		TotalPages:   r.TotalPages,
		Receivers:    make([]PublicReceiver, 0, len(r.Receivers)),
		Transmitters: make([]PublicTransmitter, 0, len(r.Transmitters)),
		Districts:    make([]PublicDistrict, 0, len(r.Districts)),
		Heat:         make([][3]float64, len(r.Heat)),
	}
	copy(out.Heat, r.Heat)
	for _, rx := range r.Receivers {
		out.Receivers = append(out.Receivers, PublicReceiver{
			Name:        rx.Name,
			Lat:         round(rx.Lat, 2),
			Lng:         round(rx.Lng, 2),
			LastHeardAt: copyTime(rx.LastHeardAt),
		})
	}
	for _, tx := range r.Transmitters {
		if tx.Channel != ChannelRFS {
			continue
		}
		var district *string
		if tx.Agency != nil {
			name := *tx.Agency
			district = &name
		}
		out.Transmitters = append(out.Transmitters, PublicTransmitter{
			Lat:      tx.Lat,
			Lng:      tx.Lng,
			Heard:    tx.Status == StatusProbable || tx.Status == StatusPossible || tx.Status == StatusDistrictHeard,
			District: district,
		})
	}
	for _, d := range r.Districts {
		if d.Channel != ChannelRFS {
			continue
		}
		out.Districts = append(out.Districts, PublicDistrict{
			Name:        d.Agency,
			Heard:       d.Pages > 0,
			Pages:       d.Pages,
			LastHeardAt: copyTime(d.LastHeardAt),
		})
	}
	return out
}

func copyTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}
