// Package stats gathers the receiver's health numbers: facts about the host
// from sysfs and procfs, and counters from the dongle loop. Everything here
// fails soft, so a developer laptop reports zero values rather than errors.
package stats

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/the-it-dept/rfspager/internal/rxproto"
)

// Paths read for host facts. Variables so tests can point them at fixtures.
var (
	ThermalPath   = "/sys/class/thermal/thermal_zone0/temp"
	LoadAvgPath   = "/proc/loadavg"
	MemInfoPath   = "/proc/meminfo"
	UptimePath    = "/proc/uptime"
	ThrottledPath = "/sys/devices/platform/soc/soc:firmware/get_throttled"
	ModelPath     = "/proc/device-tree/model"
	OSReleasePath = "/proc/sys/kernel/osrelease"
)

// Host reads the per-interval host facts. Every field is best-effort.
func Host() rxproto.HostStats {
	var h rxproto.HostStats
	if v, ok := readFloat(ThermalPath); ok {
		h.CPUTempC = v / 1000
	}
	if f := readFields(LoadAvgPath); len(f) > 0 {
		h.Load1, _ = strconv.ParseFloat(f[0], 64)
	}
	h.MemFreeMB = memAvailableMB()
	h.UptimeS = Uptime()
	h.ThrottledHex, h.UndervoltageNow, h.UndervoltageEver = throttled()
	return h
}

// Info reads the slow-changing host description for the hello frame.
func Info() rxproto.HostInfo {
	return rxproto.HostInfo{
		Model:   readString(ModelPath),
		Kernel:  readString(OSReleasePath),
		UptimeS: Uptime(),
		RTLSDR:  RTLSDRPackages(),
	}
}

// Uptime returns the host uptime in seconds, or 0 when unknown.
func Uptime() int64 {
	if f := readFields(UptimePath); len(f) > 0 {
		if v, err := strconv.ParseFloat(f[0], 64); err == nil {
			return int64(v)
		}
	}
	return 0
}

// RTLSDRPackages reports the installed librtlsdr packages as
// "name version, name version", or "" where dpkg is unavailable.
func RTLSDRPackages() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "dpkg-query", "-W", "-f=${Package} ${Version}\n",
		"librtlsdr0", "rtl-sdr-blog", "rtl-sdr").Output()
	// dpkg-query exits non-zero when any name is not installed but still
	// prints the ones that are, so keep whatever came back.
	if len(out) == 0 && err != nil {
		return ""
	}
	var parts []string
	for line := range strings.SplitSeq(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, ", ")
}

// throttled decodes the Raspberry Pi firmware get_throttled value: a hex
// string, bit 0 = undervoltage now, bit 16 = undervoltage since boot.
func throttled() (hex string, now, ever bool) {
	s := readString(ThrottledPath)
	if s == "" {
		return "", false, false
	}
	v, err := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(s), "0x"), 16, 64)
	if err != nil {
		return "", false, false
	}
	return s, v&1 != 0, v&(1<<16) != 0
}

func memAvailableMB() int {
	raw, err := os.ReadFile(MemInfoPath)
	if err != nil {
		return 0
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		if strings.HasPrefix(line, "MemAvailable:") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				kb, _ := strconv.ParseInt(f[1], 10, 64)
				return int(kb / 1024)
			}
		}
	}
	return 0
}

func readString(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	// device-tree strings are NUL-terminated.
	raw = bytes.TrimRight(raw, "\x00")
	return strings.TrimSpace(string(raw))
}

func readFields(path string) []string {
	s := readString(path)
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}

func readFloat(path string) (float64, bool) {
	s := readString(path)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}
