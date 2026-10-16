.PHONY: receiver run-receiver receiver-ui

# Build the RTL-SDR receiver agent. Requires librtlsdr installed (brew install librtlsdr).
# We pull -I / -L from pkg-config so gortlsdr's cgo can find rtl-sdr.h.
RECEIVER_CGO_CFLAGS  := $(shell pkg-config --cflags librtlsdr 2>/dev/null)
RECEIVER_CGO_LDFLAGS := $(shell pkg-config --libs librtlsdr 2>/dev/null)

# Bake the git revision into the binary so the hello frame reports it.
# `go run` doesn't always pick up VCS info via runtime/debug, so we set it
# explicitly via -ldflags.
GIT_SHA       := $(shell git rev-parse --short HEAD 2>/dev/null)
GIT_DIRTY     := $(shell git diff --quiet 2>/dev/null || echo -dirty)
RECEIVER_VERSION := $(GIT_SHA)$(GIT_DIRTY)
RECEIVER_LDFLAGS := -X github.com/the-it-dept/rfspager/internal/receiver/buildinfo.InjectedVersion=$(RECEIVER_VERSION)

# The receiver's local web UI, embedded into the binary from
# internal/receiver/localui/dist. Without it the receiver still builds and
# serves the API, with a "built without its web UI" page at /.
receiver-ui:
	cd apps/receiver-ui && npm ci && npm run build

receiver: receiver-ui
	CGO_CFLAGS="$(RECEIVER_CGO_CFLAGS)" \
	CGO_LDFLAGS="$(RECEIVER_CGO_LDFLAGS)" \
	go build -tags rtlsdr -ldflags "$(RECEIVER_LDFLAGS)" -o bin/receiver ./cmd/receiver

run-receiver:
	CGO_CFLAGS="$(RECEIVER_CGO_CFLAGS)" \
	CGO_LDFLAGS="$(RECEIVER_CGO_LDFLAGS)" \
	go run -tags rtlsdr -ldflags "$(RECEIVER_LDFLAGS)" ./cmd/receiver | tee -a pager.log
