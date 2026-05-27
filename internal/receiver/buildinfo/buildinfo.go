// Package buildinfo exposes the receiver binary's identity for the hello
// frame: VCS revision, OS, arch, hostname. Pulls directly from runtime —
// `go run` and `go build` both populate this automatically (since Go 1.18).
package buildinfo

import (
	"os"
	"runtime"
	"runtime/debug"
)

// Info is the snapshot sent to the server on receiver connect.
type Info struct {
	Version  string `json:"version"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Hostname string `json:"hostname"`
}

// InjectedVersion is set by the linker via -ldflags at build time:
//
//	-ldflags "-X github.com/the-it-dept/rfspager/internal/receiver/buildinfo.InjectedVersion=$(git rev-parse --short HEAD)"
//
// It takes precedence over the runtime/debug VCS info, which is missing
// when the binary is launched with `go run`.
var InjectedVersion = ""

func Get() Info {
	host, _ := os.Hostname()
	return Info{
		Version:  version(),
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Hostname: host,
	}
}

func version() string {
	if InjectedVersion != "" {
		return InjectedVersion
	}
	return vcsVersion()
}

// vcsVersion returns the git revision baked into the binary by `go build`,
// formatted as "<short-sha>[-dirty]". If VCS info isn't present (e.g. test
// binary), returns "unknown".
func vcsVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	var rev, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				modified = "-dirty"
			}
		}
	}
	if rev == "" {
		return "unknown"
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	return rev + modified
}
