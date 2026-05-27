//go:build !rtlsdr

package sdr

import "errors"

// errStub is returned when the receiver is built without the rtlsdr tag.
var errStub = errors.New("rtl-sdr support not compiled in; build with -tags rtlsdr (requires librtlsdr)")

// Open is a stub used when the binary is built without the rtlsdr tag.
func Open(index int) (Device, error) { return nil, errStub }

// Count returns 0 when the rtlsdr tag isn't set.
func Count() int { return 0 }

// Enumerate returns an empty list when the rtlsdr tag isn't set.
func Enumerate() []DongleInfo { return nil }
