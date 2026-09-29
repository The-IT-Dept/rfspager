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
"$TMP/receiver" --version >/dev/null 2>&1 || true
[ -s "$BIN_DIR/receiver" ] && cp -f "$BIN_DIR/receiver" "$BIN_DIR/receiver.prev"
mv -f "$TMP/receiver" "$BIN_DIR/receiver"
say "Installed $BIN_DIR/receiver"

# --- Configuration ---------------------------------------------------------
# Written fresh each run so an upgrade cannot leave stale legacy keys behind:
# the current receiver refuses to start if RECEIVER_LEGACY_* are present.
say "Writing $ENV_FILE"
umask 077
cat > "$ENV_FILE" <<EOF
# RFSPager receiver — written by the installer on $(date -u +%Y-%m-%dT%H:%M:%SZ)
# Re-run the install command to rotate this file.
RECEIVER_SERVER_URL=$SERVER_URL
RECEIVER_API_KEY=$TOKEN
TZ=Australia/Sydney
EOF
chmod 600 "$ENV_FILE"

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
