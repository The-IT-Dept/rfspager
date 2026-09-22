package install

import (
	_ "embed"
	"net/http"
	"strings"
	"text/template"
)

//go:embed install.sh
var installScript string

var scriptTmpl = template.Must(template.New("install.sh").Parse(installScript))

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

// publicBase prefers the configured origin and otherwise reconstructs the one
// the caller used, so the script points back at the host the operator typed
// rather than at an internal address.
func (h *Handlers) publicBase(r *http.Request) string {
	if h.baseURL != "" {
		return h.baseURL
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
