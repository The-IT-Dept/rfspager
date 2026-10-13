package stats

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHostFailsSoftWithoutProcfs(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []*string{&ThermalPath, &LoadAvgPath, &MemInfoPath, &UptimePath, &ThrottledPath, &ModelPath, &OSReleasePath} {
		old := *p
		*p = filepath.Join(dir, "missing")
		t.Cleanup(func() { *p = old })
	}
	h := Host()
	if h.CPUTempC != 0 || h.Load1 != 0 || h.MemFreeMB != 0 || h.UptimeS != 0 || h.ThrottledHex != "" || h.UndervoltageNow || h.UndervoltageEver {
		t.Errorf("expected zero values, got %+v", h)
	}
	if i := Info(); i.Model != "" || i.Kernel != "" || i.UptimeS != 0 {
		t.Errorf("expected empty host info, got %+v", i)
	}
}

func TestHostReadsFixtures(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	set := func(p *string, v string) {
		old := *p
		*p = v
		t.Cleanup(func() { *p = old })
	}
	set(&ThermalPath, write("temp", "48234\n"))
	set(&LoadAvgPath, write("loadavg", "0.42 0.30 0.25 1/123 4567\n"))
	set(&MemInfoPath, write("meminfo", "MemTotal:  3884000 kB\nMemAvailable:  2048000 kB\n"))
	set(&UptimePath, write("uptime", "12345.67 24000.00\n"))
	set(&ThrottledPath, write("get_throttled", "0x50001\n"))
	set(&ModelPath, write("model", "Raspberry Pi 4 Model B Rev 1.5\x00"))
	set(&OSReleasePath, write("osrelease", "6.1.21-v8+\n"))

	h := Host()
	if h.CPUTempC < 48.2 || h.CPUTempC > 48.3 {
		t.Errorf("temp %v", h.CPUTempC)
	}
	if h.Load1 != 0.42 || h.MemFreeMB != 2000 || h.UptimeS != 12345 {
		t.Errorf("load/mem/uptime: %+v", h)
	}
	if h.ThrottledHex != "0x50001" || !h.UndervoltageNow || !h.UndervoltageEver {
		t.Errorf("throttled: %+v", h)
	}
	i := Info()
	if i.Model != "Raspberry Pi 4 Model B Rev 1.5" || i.Kernel != "6.1.21-v8+" || i.UptimeS != 12345 {
		t.Errorf("info: %+v", i)
	}
}

func TestDongleSnapshotComputesLoadAndResets(t *testing.T) {
	var d Dongle
	// 10 buffers of 16384 samples at 1.92 MS/s = 85.3 ms of IQ; 17 ms of
	// processing is a load of 0.2.
	for range 10 {
		d.Buffer(0, 16384, 1700*time.Microsecond)
	}
	d.Buffer(3, 16384, 1700*time.Microsecond)
	buffers, dropped, gaps, load := d.Snapshot(1_920_000)
	if buffers != 11 || dropped != 3 || gaps != 1 {
		t.Errorf("buffers=%d dropped=%d gaps=%d", buffers, dropped, gaps)
	}
	if load < 0.19 || load > 0.21 {
		t.Errorf("dsp load %v, want ~0.2", load)
	}
	if b, _, _, l := d.Snapshot(1_920_000); b != 0 || l != 0 {
		t.Errorf("counters not reset: buffers=%d load=%v", b, l)
	}
}
