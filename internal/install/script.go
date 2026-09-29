package install

import (
	_ "embed"
	"net/http"
	"strings"
	"text/template"
)

//go:embed install.sh
var installScript string

// shq is exposed to the template so the rendered script can single-quote the
// one value it does not control.
var scriptTmpl = template.Must(template.New("install.sh").Funcs(
	template.FuncMap{"shq": shellQuote}).Parse(installScript))

// Script serves the installer. It is deliberately unauthenticated: the script
// is not a secret, it is a public recipe. The receiver token the operator
// supplies is what grants anything, and the script validates it before it
// changes a single thing on the host.
//
// GET /api/install
func (h *Handlers) Script(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// curl | bash is only as safe as the transport, so make downgrades harder.
	w.Header().Set("Strict-Transport-Security", "max-age=31536000")
	_ = scriptTmpl.Execute(w, map[string]string{"BaseURL": h.publicBase(r)})
}

// ConfiguredBaseURL is set once at startup from PUBLIC_BASE_URL. It lets code
// outside this package (the mobile systems endpoint, which shows an owner their
// install command) render the same origin without threading config through two
// more constructors — the same pattern main already uses for recvhub.PodID.
var ConfiguredBaseURL string

// CommandFor renders the one-liner a system owner runs on their Pi. Kept here
// so the command and the script that serves it cannot drift apart.
//
// Every interpolated value is single-quoted: BaseURLFor returns a value built
// from request headers, and an unquoted `https://evil"; id; echo "` is split by
// the shell into three commands. The token is quoted too, so a key that starts
// with `-` or contains a space cannot be read as an option or a second
// argument. shellQuote keeps the rendered command readable when the value is
// the ordinary case.
func CommandFor(r *http.Request, token string) string {
	return "curl -fsSL " + shellQuote(BaseURLFor(r, ConfiguredBaseURL)) +
		"/api/install | sudo bash -s -- " + shellQuote(token)
}

// shellQuote wraps v in single quotes for POSIX sh. A literal single quote is
// emitted as '\” (close, escaped quote, reopen) so the result is safe for any
// byte sequence a header or API key can carry.
func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

// BaseURLFor prefers the configured origin and otherwise reconstructs the one
// the caller used, so the script points back at the host the operator typed
// rather than at an internal address.
func BaseURLFor(r *http.Request, configured string) string {
	if configured != "" {
		return configured
	}
	scheme := "https"
	if f := r.Header.Get("X-Forwarded-Proto"); f != "" {
		scheme = f
	} else if r.TLS == nil && strings.HasPrefix(r.Host, "localhost") {
		scheme = "http"
	}
	host := r.Host
	if f := r.Header.Get("X-Forwarded-Host"); f != "" {
		host = f
	}
	return scheme + "://" + host
}

func (h *Handlers) publicBase(r *http.Request) string {
	return BaseURLFor(r, h.baseURL)
}
