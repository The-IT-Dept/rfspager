package rxproto

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDefaultsAreValid(t *testing.T) {
	if errs := Defaults().Validate(); errs != nil {
		t.Fatalf("defaults should validate, got %v", errs)
	}
}

func TestDefaultsMatchHardcodedChannels(t *testing.T) {
	d := Defaults()
	if len(d.Channels) != 2 || d.Channels[0].FreqHz != 148_587_500 || d.Channels[1].FreqHz != 148_962_500 {
		t.Fatalf("defaults must reproduce the hardcoded RFS/FRNSW channels, got %+v", d.Channels)
	}
	centre, ok := d.CentreHz()
	if !ok || centre != 148_775_000 {
		t.Fatalf("centre should be the midpoint 148775000, got %d ok=%v", centre, ok)
	}
	if d.SampleRate%AudioRate != 0 {
		t.Fatalf("default sample rate must divide by the audio rate")
	}
}

func TestValidateRejectsOutOfRange(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*RuntimeConfig)
		key  string
	}{
		{"negative gain", func(c *RuntimeConfig) { c.TunerGainTenthsDB = -1 }, "tuner_gain_tenths_db"},
		{"huge gain", func(c *RuntimeConfig) { c.TunerGainTenthsDB = 601 }, "tuner_gain_tenths_db"},
		{"ppm", func(c *RuntimeConfig) { c.PPM = 201 }, "ppm"},
		{"sample rate low", func(c *RuntimeConfig) { c.SampleRate = 100_000 }, "sample_rate"},
		{"sample rate not multiple", func(c *RuntimeConfig) { c.SampleRate = 2_400_000 }, "sample_rate"},
		{"dongle index", func(c *RuntimeConfig) { c.DongleIndex = -2 }, "dongle_index"},
		{"log level", func(c *RuntimeConfig) { c.LogLevel = "trace" }, "log_level"},
		{"stats interval", func(c *RuntimeConfig) { c.StatsIntervalS = 1 }, "stats_interval_s"},
		{"no channels", func(c *RuntimeConfig) { c.Channels = nil }, "channels"},
		{"none enabled", func(c *RuntimeConfig) {
			for i := range c.Channels {
				c.Channels[i].Enabled = false
			}
		}, "channels"},
		{"duplicate label", func(c *RuntimeConfig) { c.Channels[1].Label = "RFS" }, "channels[1].label"},
		{"blank label", func(c *RuntimeConfig) { c.Channels[0].Label = " " }, "channels[0].label"},
		{"freq out of range", func(c *RuntimeConfig) { c.Channels[0].FreqHz = 1000 }, "channels[0].freq_hz"},
		{"span too wide", func(c *RuntimeConfig) { c.Channels[1].FreqHz = 150_500_000 }, "channels"},
		{"too many", func(c *RuntimeConfig) {
			for i := range 3 {
				c.Channels = append(c.Channels, ChannelConfig{Label: strings.Repeat("x", i+1), FreqHz: 148_700_000, Enabled: true})
			}
		}, "channels"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Defaults()
			tc.mut(&c)
			errs := c.Validate()
			if errs == nil {
				t.Fatalf("expected a validation error for %s", tc.key)
			}
			if _, ok := errs[tc.key]; !ok {
				t.Fatalf("expected error keyed %q, got %v", tc.key, errs)
			}
		})
	}
}

func TestValidateAcceptsDisabledWideChannel(t *testing.T) {
	// A disabled channel far away must not count towards the span.
	c := Defaults()
	c.Channels = append(c.Channels, ChannelConfig{Label: "FAR", FreqHz: 160_000_000, Enabled: false})
	if errs := c.Validate(); errs != nil {
		t.Fatalf("disabled channel should be ignored for span, got %v", errs)
	}
}

func TestNormalizeFillsDefaults(t *testing.T) {
	cfg, err := Normalize([]byte(`{"tuner_gain_tenths_db": 496}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TunerGainTenthsDB != 496 {
		t.Fatalf("explicit key not applied: %+v", cfg)
	}
	if cfg.SampleRate != 1_920_000 || cfg.LogLevel != "info" || len(cfg.Channels) != 2 {
		t.Fatalf("missing keys should take defaults: %+v", cfg)
	}
}

func TestNormalizeReplacesChannels(t *testing.T) {
	cfg, err := Normalize([]byte(`{"channels":[{"label":"ONLY","freq_hz":148587500,"enabled":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Channels) != 1 || cfg.Channels[0].Label != "ONLY" {
		t.Fatalf("channels should replace the defaults wholesale: %+v", cfg.Channels)
	}
}

func TestNormalizeEmptyIsDefaults(t *testing.T) {
	cfg, err := Normalize(nil)
	if err != nil {
		t.Fatal(err)
	}
	if errs := cfg.Validate(); errs != nil {
		t.Fatalf("empty input should produce valid defaults: %v", errs)
	}
}

func TestNormalizeRejectsGarbage(t *testing.T) {
	if _, err := Normalize([]byte(`{"ppm": "ten"}`)); err == nil {
		t.Fatal("expected a decode error")
	}
}

func TestLiveApplicable(t *testing.T) {
	old := Defaults()
	live := old
	live.TunerGainTenthsDB = 496
	live.PPM = 3
	live.LogLevel = "debug"
	live.PostAll = true
	live.StatsIntervalS = 30
	if !LiveApplicable(old, live) {
		t.Fatal("gain/ppm/log level/post_all/stats interval should apply live")
	}
	for _, mut := range []func(*RuntimeConfig){
		func(c *RuntimeConfig) { c.SampleRate = 1_152_000 },
		func(c *RuntimeConfig) { c.DongleSerial = "RFS00002" },
		func(c *RuntimeConfig) { c.DongleIndex = 1 },
		func(c *RuntimeConfig) { c.Channels[0].Enabled = false },
		func(c *RuntimeConfig) { c.Channels = c.Channels[:1] },
	} {
		c := Defaults()
		mut(&c)
		if LiveApplicable(old, c) {
			t.Fatalf("change %+v should need a dongle restart", c)
		}
	}
}

func TestRoundTripJSON(t *testing.T) {
	d := Defaults()
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Normalize(b)
	if err != nil {
		t.Fatal(err)
	}
	if back.SampleRate != d.SampleRate || len(back.Channels) != len(d.Channels) || back.Channels[1] != d.Channels[1] {
		t.Fatalf("round trip mismatch: %+v vs %+v", back, d)
	}
}

func TestFrameEnvelope(t *testing.T) {
	cmd := Command{ID: "c1", Type: CmdUpdate, Params: json.RawMessage(`{"version":"v5.5.0"}`)}
	data, _ := json.Marshal(cmd)
	f := Frame{Type: FrameCommand, ID: cmd.ID, Data: data}
	b, _ := json.Marshal(f)
	var got Frame
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	var gotCmd Command
	if err := json.Unmarshal(got.Data, &gotCmd); err != nil {
		t.Fatal(err)
	}
	var p UpdateParams
	if err := json.Unmarshal(gotCmd.Params, &p); err != nil || p.Version != "v5.5.0" {
		t.Fatalf("params did not survive: %v %+v", err, p)
	}
}
