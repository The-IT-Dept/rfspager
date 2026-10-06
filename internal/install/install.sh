#!/usr/bin/env bash
#
# RFSPager receiver installer.
#
#   curl -fsSL {{.BaseURL}}/api/install | sudo bash -s -- <token>
#
# Everything is checked before anything is changed: the token is validated
# against the server, the platform is confirmed, and the binary's checksum is
# verified before it is put in place. Re-running is safe and is the supported
# way to upgrade.
set -euo pipefail

# Single-quoted: the value is assembled from request headers and may carry a
# double quote, a semicolon or a command substitution. (Braces in this comment
# would be read as template actions, hence the spelling.)
SERVER_URL={{shq .BaseURL}}
BIN_DIR=/opt/rfspager
ETC_DIR=/etc/rfspager
UNIT=/etc/systemd/system/rfspager-receiver.service
ENV_FILE="$ETC_DIR/receiver.env"

say()  { printf '\033[1;32m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m==>\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31mx\033[0m %s\n' "$*" >&2; exit 1; }

# The app renders the token as --api-key=<token>; the admin/mobile one-liner
# passes it bare. Accept both, and strip a leading --api-key= so the value sent
# as the apikey header is the key itself.
TOKEN="${1:-}"
TOKEN="${TOKEN#--api-key=}"
[ -n "$TOKEN" ] || die "Usage: curl -fsSL $SERVER_URL/api/install | sudo bash -s -- <token>"
[ "$(id -u)" = "0" ] || die "This needs root. Pipe it to 'sudo bash' rather than 'bash'."
command -v systemctl >/dev/null || die "No systemd here; this installer only supports systemd hosts."
command -v curl >/dev/null || die "curl is required."

case "$(uname -m)" in
  aarch64|arm64) PLATFORM=linux-arm64 ;;
  x86_64|amd64)  PLATFORM=linux-amd64 ;;
  armv7l|armv6l) PLATFORM=linux-armv7 ;;
  *) die "Unsupported architecture: $(uname -m)" ;;
esac
say "Platform: $PLATFORM"

# --- Validate the token before touching the system -------------------------
say "Checking the token with $SERVER_URL"
ME="$(curl -fsSL --max-time 30 -H "apikey: $TOKEN" "$SERVER_URL/api/receiver/me" 2>/dev/null)" \
  || die "The server rejected that token. Copy it again from the app; it belongs to one receiver."
SYSTEM_NAME="$(printf '%s' "$ME" | sed -n 's/.*"name":"\([^"]*\)".*/\1/p')"
say "Token is valid for: ${SYSTEM_NAME:-unknown receiver}"

# --- Dependencies ----------------------------------------------------------
# The binary links against librtlsdr at runtime; rtl-sdr brings the udev rules.
if command -v apt-get >/dev/null; then
  say "Installing rtl-sdr dependencies"
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq librtlsdr0 libusb-1.0-0 rtl-sdr ca-certificates >/dev/null
else
  warn "No apt-get; make sure librtlsdr and libusb are installed yourself."
fi

# The kernel's DVB-T driver claims the dongle on boot and must be kept off it.
if [ ! -f /etc/modprobe.d/blacklist-rtlsdr.conf ]; then
  say "Blacklisting the kernel DVB-T driver (it claims the dongle otherwise)"
  cat > /etc/modprobe.d/blacklist-rtlsdr.conf <<'EOF'
# The RTL-SDR dongle is claimed by the DVB-T driver unless this is in place.
blacklist dvb_usb_rtl28xxu
blacklist rtl2832
blacklist rtl2830
EOF
fi
modprobe -r dvb_usb_rtl28xxu 2>/dev/null || true

# --- Previous Docker-based agent -------------------------------------------
# The old installer (rfspager-docs/agent.sh) ran the decoder as Docker
# containers, rfspager-frnsw / rfspager-rfs from the rfspager-client image,
# defined in /opt/rfspager/docker-compose.yml with /dev/bus/usb passed through
# and restart: unless-stopped. Left in place they hold the dongle and return on
# every boot, so take them down. Docker itself is left installed.
LEGACY_COMPOSE="$BIN_DIR/docker-compose.yml"
if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  if [ -f "$LEGACY_COMPOSE" ]; then
    say "Stopping the previous Docker-based agent"
    if docker compose version >/dev/null 2>&1; then
      docker compose -f "$LEGACY_COMPOSE" down --remove-orphans >/dev/null 2>&1 || true
    elif command -v docker-compose >/dev/null 2>&1; then
      docker-compose -f "$LEGACY_COMPOSE" down --remove-orphans >/dev/null 2>&1 || true
    fi
  fi
  # Catch containers started outside that compose file too. Match on the image
  # *name* from the default listing, never the image ID: an ID can be shared
  # with unrelated tags, and only the old agent may be touched here.
  LEGACY_REPO=ghcr.io/the-it-dept/rfspager-client
  LEGACY_IDS="$( { docker ps -aq --filter name='^rfspager-frnsw$'
                   docker ps -aq --filter name='^rfspager-rfs$'
                   docker ps -a --no-trunc 2>/dev/null \
                     | awk -v repo="$LEGACY_REPO" 'NR > 1 && (index($2, repo ":") == 1 || index($2, repo "@") == 1 || $2 == repo) { print substr($1, 1, 12) }'
                 } 2>/dev/null | sort -u )"
  if [ -n "$LEGACY_IDS" ]; then
    say "Removing previous agent containers"
    # shellcheck disable=SC2086
    docker rm -f $LEGACY_IDS >/dev/null
  fi
  # Untag by name:tag (no -f) so an image shared with another tag survives.
  # Docker 29 lists a single IMAGE (repo:tag) column; older releases split
  # REPOSITORY and TAG. Read the header to tell which.
  LEGACY_REFS="$(docker images "$LEGACY_REPO" 2>/dev/null | awk '
    NR == 1 { split_cols = ($1 == "REPOSITORY"); next }
    split_cols { print ($2 == "<none>" ? $3 : $1 ":" $2); next }
    { print $1 }')"
  if [ -n "$LEGACY_REFS" ]; then
    say "Removing the previous agent image"
    # shellcheck disable=SC2086
    docker rmi $LEGACY_REFS >/dev/null 2>&1 || warn "Could not remove the old rfspager-client image; it is unused and safe to delete."
  fi
fi
if [ -f "$LEGACY_COMPOSE" ]; then
  # It carries the old PagerMon key; keep it out of the way rather than live.
  mv -f "$LEGACY_COMPOSE" "$LEGACY_COMPOSE.replaced-$(date -u +%Y%m%d%H%M%S)"
  chmod 600 "$LEGACY_COMPOSE".replaced-* 2>/dev/null || true
  say "Set aside the old docker-compose.yml"
fi

# --- Binary ----------------------------------------------------------------
mkdir -p "$BIN_DIR" "$ETC_DIR"
chmod 755 "$BIN_DIR"; chmod 750 "$ETC_DIR"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT

say "Downloading the receiver"
curl -fsSL --max-time 300 -H "apikey: $TOKEN" \
  -o "$TMP/receiver" "$SERVER_URL/api/binaries/receiver/$PLATFORM" \
  || die "Could not download the receiver binary."

if curl -fsSL --max-time 60 -H "apikey: $TOKEN" \
     -o "$TMP/receiver.sha256" "$SERVER_URL/api/binaries/receiver/$PLATFORM.sha256" 2>/dev/null; then
  EXPECT="$(awk '{print $1}' "$TMP/receiver.sha256")"
  ACTUAL="$(sha256sum "$TMP/receiver" | awk '{print $1}')"
  [ "$EXPECT" = "$ACTUAL" ] || die "Checksum mismatch — refusing to install. Expected $EXPECT, got $ACTUAL."
  say "Checksum verified"
else
  warn "No checksum published for this release; skipping verification."
fi

chmod 755 "$TMP/receiver"
# Confirm librtlsdr and friends resolve without executing it: the receiver
# takes no arguments, so running it here would start a second decoder.
if command -v ldd >/dev/null && ldd "$TMP/receiver" 2>/dev/null | grep -q "not found"; then
  ldd "$TMP/receiver" | grep "not found" >&2 || true
  die "The receiver is missing shared libraries (listed above)."
fi
[ -s "$BIN_DIR/receiver" ] && cp -f "$BIN_DIR/receiver" "$BIN_DIR/receiver.prev"
mv -f "$TMP/receiver" "$BIN_DIR/receiver"
say "Installed $BIN_DIR/receiver"

# --- Configuration ---------------------------------------------------------
# Written fresh each run so an upgrade cannot leave stale legacy keys behind:
# the current receiver refuses to start if RECEIVER_LEGACY_* are present.
#
# Per-site radio tuning is carried over, though: an upgrade must not reset a
# gain or dongle choice someone tuned by hand.
TUNING=""
if [ -f "$ENV_FILE" ]; then
  TUNING="$(grep -E '^(RECEIVER_TUNER_GAIN|RECEIVER_DONGLE_INDEX|RECEIVER_DONGLE_SERIAL|RECEIVER_PPM|RECEIVER_SAMPLE_RATE|RECEIVER_POST_ALL)=' "$ENV_FILE" || true)"
fi
say "Writing $ENV_FILE"
umask 077
cat > "$ENV_FILE" <<EOF
# RFSPager receiver — written by the installer on $(date -u +%Y-%m-%dT%H:%M:%SZ)
# Re-run the install command to rotate this file.
RECEIVER_SERVER_URL=$SERVER_URL
RECEIVER_API_KEY=$TOKEN
TZ=Australia/Sydney
EOF
if [ -n "$TUNING" ]; then
  printf '\n# Kept from the previous configuration\n%s\n' "$TUNING" >> "$ENV_FILE"
  say "Kept radio tuning: $(printf '%s' "$TUNING" | cut -d= -f1 | tr '\n' ' ')"
fi
chmod 600 "$ENV_FILE"

# A fan-out file overrides the server and key above; say so rather than let a
# stale one quietly win.
if [ -f "$ETC_DIR/endpoints.json" ]; then
  warn "$ETC_DIR/endpoints.json exists and takes precedence over $ENV_FILE."
  warn "Make sure it points at $SERVER_URL with this receiver's key, or remove it."
fi

# --- Service ---------------------------------------------------------------
say "Installing the systemd unit"
cat > "$UNIT" <<'EOF'
[Unit]
Description=RFSPager RTL-SDR receiver
Documentation=https://github.com/the-it-dept/rfspager
After=network-online.target
Wants=network-online.target
# Never stop retrying: the USB dongle can be slow to enumerate at boot.
StartLimitIntervalSec=0

[Service]
Type=simple
EnvironmentFile=/etc/rfspager/receiver.env
ExecStart=/opt/rfspager/receiver
WorkingDirectory=/opt/rfspager
Restart=always
RestartSec=5s
StandardOutput=journal
StandardError=journal
SyslogIdentifier=rfspager-receiver

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable rfspager-receiver >/dev/null 2>&1 || true
systemctl restart rfspager-receiver

# --- Validate --------------------------------------------------------------
say "Waiting for the receiver to come up"
for _ in $(seq 1 15); do
  sleep 2
  systemctl is-active --quiet rfspager-receiver && break
done

if ! systemctl is-active --quiet rfspager-receiver; then
  warn "The service is not running. The last few log lines:"
  journalctl -u rfspager-receiver -n 20 --no-pager || true
  die "Install finished but the receiver did not start."
fi

# The receiver dials the server over a websocket; confirm the server has seen it
# rather than trusting that the process merely started.
ONLINE=no
for _ in $(seq 1 10); do
  if curl -fsSL --max-time 15 -H "apikey: $TOKEN" "$SERVER_URL/api/receiver/me" >/dev/null 2>&1; then
    if journalctl -u rfspager-receiver -n 50 --no-pager 2>/dev/null | grep -qiE "welcome|connected|ws transport"; then
      ONLINE=yes; break
    fi
  fi
  sleep 3
done

echo
say "Receiver installed for: ${SYSTEM_NAME:-unknown receiver}"
if [ "$ONLINE" = yes ]; then
  say "It is connected to $SERVER_URL"
else
  warn "It is running but has not confirmed a connection yet."
  warn "Watch it with: journalctl -u rfspager-receiver -f"
fi
echo
echo "  status   systemctl status rfspager-receiver"
echo "  logs     journalctl -u rfspager-receiver -f"
echo "  upgrade  re-run this same install command"
echo
