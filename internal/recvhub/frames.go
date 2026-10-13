// Package recvhub holds the server-side websocket hub that talks to receiver
// agents. It owns the per-pod in-memory conn map, the cross-pod NOTIFY
// plumbing that lets any pod push config and commands to the pod holding a
// given receiver's socket, and the stats/log brokers that fan receiver
// reports out to admin and owner streams.
package recvhub

import "github.com/the-it-dept/rfspager/internal/rxproto"

// The frame envelope and every payload are defined once in rxproto so the
// receiver agent and this hub cannot drift. The names below predate that
// package and are kept as aliases so existing callers (including the
// PageHandler signature cmd/server wires up) keep compiling.
type (
	Frame          = rxproto.Frame
	DongleInfo     = rxproto.DongleInfo
	HelloPayload   = rxproto.Hello
	PagePayload    = rxproto.Page
	LogPayload     = rxproto.Log
	WelcomePayload = rxproto.Welcome
	AckPayload     = rxproto.Ack
	ConfigPayload  = rxproto.ConfigPayload
)
