package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/the-it-dept/rfspager/internal/receiver/config"
	"github.com/the-it-dept/rfspager/internal/receiver/control"
	"github.com/the-it-dept/rfspager/internal/receiver/legacy"
	"github.com/the-it-dept/rfspager/internal/receiver/localui"
	"github.com/the-it-dept/rfspager/internal/receiver/sdr"
	"github.com/the-it-dept/rfspager/internal/receiver/stats"
	"github.com/the-it-dept/rfspager/internal/receiver/wsclient"
	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// upstream is one configured endpoint with its transport, for the local
// UI's Connections and Settings tabs.
type upstream struct {
	ep     config.Endpoint
	ws     *wsclient.Client
	legacy *legacy.Client
}

// uiSources wires the local UI to the running receiver.
func uiSources(
	mgr *control.Manager, pipe *pipeline, ups []upstream, primary *wsclient.Client,
	system *atomic.Pointer[localui.System], startVersion int, startSource string,
) localui.Sources {
	return localui.Sources{
		System:     system.Load,
		DongleOpen: pipe.open.Load,
		Host:       stats.Host,
		Endpoints: func() []localui.Endpoint {
			out := make([]localui.Endpoint, 0, len(ups))
			for _, u := range ups {
				out = append(out, endpointStatus(u, primary))
			}
			return out
		},
		Settings: func() localui.Settings {
			v := mgr.Version()
			src := startSource
			if v != startVersion {
				src = fmt.Sprintf("server (revision %d)", v)
			}
			eps := make([]localui.EndpointInfo, 0, len(ups))
			for _, u := range ups {
				eps = append(eps, localui.EndpointInfo{
					Name: u.ep.Name, Mode: u.ep.Mode, URL: u.ep.URL, Primary: u.ws != nil && u.ws == primary,
				})
			}
			return localui.Settings{
				Config: mgr.Config(), ConfigVersion: v, ConfigSource: src,
				Host: stats.Info(), Dongles: dongles(primary),
				Endpoints: eps, StateDir: config.StateDir(),
			}
		},
	}
}

func endpointStatus(u upstream, primary *wsclient.Client) localui.Endpoint {
	e := localui.Endpoint{Name: u.ep.Name, Mode: u.ep.Mode, URL: u.ep.URL}
	switch {
	case u.ws != nil:
		st := u.ws.Status()
		e.Primary = u.ws == primary
		e.Connected = st.Connected
		e.ConnectedSince = msPtr(st.ConnectedSince)
		e.RTTMs = st.Link.RTTMs
		e.ReconnectsTotal = st.Link.ReconnectsTotal
		e.QueueDepth = st.Link.QueueDepth
		e.DroppedFramesTotal = st.Link.DroppedFramesTotal
		e.PagesSent, e.PagesFailed = st.PagesSent, st.PagesDropped
		e.LastPageAt = msPtr(st.LastPageAt)
		e.LastError, e.LastErrorAt = st.LastError, msPtr(st.LastErrorAt)
	case u.legacy != nil:
		st := u.legacy.Status()
		e.Connected = st.LastOK
		e.PagesSent, e.PagesFailed = st.PagesSent, st.PagesFailed
		e.LastPageAt = msPtr(st.LastPageAt)
		e.LastError, e.LastErrorAt = st.LastError, msPtr(st.LastErrorAt)
	}
	return e
}

func msPtr(ms int64) *int64 {
	if ms == 0 {
		return nil
	}
	return &ms
}

// Enumerating dongles reads their USB strings, so the Settings tab reuses a
// list for a minute rather than touching USB on every request.
var dongleCache struct {
	sync.Mutex
	at   time.Time
	list []rxproto.DongleInfo
}

func dongles(primary *wsclient.Client) []rxproto.DongleInfo {
	dongleCache.Lock()
	defer dongleCache.Unlock()
	if dongleCache.list != nil && time.Since(dongleCache.at) < time.Minute {
		return dongleCache.list
	}
	var list []rxproto.DongleInfo
	if primary != nil {
		list = primary.Dongles()
	} else {
		for _, d := range sdr.Enumerate() {
			list = append(list, rxproto.DongleInfo{
				Index: d.Index, Name: d.Name, Manufacturer: d.Manufacturer, Product: d.Product, Serial: d.Serial,
			})
		}
	}
	if list == nil {
		list = []rxproto.DongleInfo{}
	}
	dongleCache.list, dongleCache.at = list, time.Now()
	return list
}
