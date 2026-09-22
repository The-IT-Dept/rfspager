// Package config loads the receiver agent's runtime config from the environment.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	// ServerURL is the rfspager base URL, e.g. https://rfspager.example.com
	ServerURL string

	// APIKey identifies this receiver against the rfspager server. Every
	// decoded page is posted under this key — frequency is no longer a
	// per-system property; the receiver owns the freq list.
	APIKey string

	// SampleRate is the RTL-SDR ADC rate. 1.92 MS/s is a clean rate that
	// divides nicely down to 38.4 kHz per channel (POCSAG baud-rate friendly).
	SampleRate uint32

	// TunerGain in tenths of a dB. 0 means use automatic gain.
	TunerGain int

	// CorrectionPPM is the dongle's frequency error correction.
	CorrectionPPM int

	// DongleIndex pins to a specific dongle by enumeration index. -1 means
	// "no pin, use index 0". Indices renumber when you re-plug dongles, so
	// prefer DongleSerial for anything you want stable across reboots.
	DongleIndex int

	// DongleSerial pins to a specific dongle by USB serial number (case-
	// insensitive substring match). Takes precedence over DongleIndex when
	// set. Examples: "RFS00005", "00000001".
	DongleSerial string

	// DisableSpectrum skips the per-buffer FFT, JSON serialize and WS
	// spectrum frame. Useful for isolating whether the waterfall load is
	// contributing to USB stability issues on marginal hardware.
	DisableSpectrum bool

	// PostAll bypasses the receiver-side shouldPost content filter so
	// every decoded page (including beacons / numeric / noise-shaped
	// alpha) gets sent upstream. Useful for end-to-end pipeline tests
	// where you want guaranteed traffic without waiting for a real
	// dispatch.
	PostAll bool
}

func Load() (*Config, error) {
	cfg := &Config{
		ServerURL:       getEnv("RECEIVER_SERVER_URL", "http://localhost:8080"),
		APIKey:          strings.Trim(strings.TrimSpace(os.Getenv("RECEIVER_API_KEY")), `"'`),
		SampleRate:      uint32(getInt("RECEIVER_SAMPLE_RATE", 1_920_000)),
		TunerGain:       getInt("RECEIVER_TUNER_GAIN", 0),
		CorrectionPPM:   getInt("RECEIVER_PPM", 0),
		DongleIndex:     getInt("RECEIVER_DONGLE_INDEX", -1),
		DongleSerial:    strings.TrimSpace(os.Getenv("RECEIVER_DONGLE_SERIAL")),
		DisableSpectrum: os.Getenv("RECEIVER_DISABLE_SPECTRUM") == "1",
		PostAll:         os.Getenv("RECEIVER_POST_ALL") == "1",
	}
	if os.Getenv("RECEIVER_LEGACY_URL") != "" || os.Getenv("RECEIVER_LEGACY_SYSTEM_UUID") != "" {
		return nil, fmt.Errorf("legacy ingress has been removed; configure RECEIVER_SERVER_URL and a newly provisioned RECEIVER_API_KEY")
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("RECEIVER_API_KEY is required")
	}
	u, err := url.Parse(cfg.ServerURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid RECEIVER_SERVER_URL")
	}
	loopback := u.Hostname() == "localhost"
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		loopback = ip.IsLoopback()
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return nil, fmt.Errorf("RECEIVER_SERVER_URL must use HTTPS; HTTP is allowed only on loopback")
	}

	return cfg, nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
