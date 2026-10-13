#!/usr/bin/env bash
#
# Build the arm64 receiver with the same Dockerfile the release workflow
# uses, copy it to a Raspberry Pi and restart the service there.
#
#   scripts/deploy-receiver.sh rfspager@192.168.15.52
#   scripts/deploy-receiver.sh root@100.79.78.79 --version v5.5.0-rc1
#
# The version baked into the binary (what the hello frame reports) defaults
# to `git describe --always --dirty`. The previous binary is kept on the Pi
# as /opt/rfspager/receiver.prev; put it back with
#   ssh <target> 'sudo mv /opt/rfspager/receiver.prev /opt/rfspager/receiver && sudo systemctl restart rfspager-receiver'
#
# Needs: docker with buildx (and QEMU for arm64 if this is not an arm64
# host), ssh and scp access to the target. Run from anywhere inside the repo.
set -euo pipefail

usage() {
  echo "usage: $0 <ssh-target> [--version X]" >&2
  exit 2
}

TARGET="${1:-}"
[ -n "$TARGET" ] || usage
shift
VERSION=""
while [ $# -gt 0 ]; do
  case "$1" in
    --version) [ $# -ge 2 ] || usage; VERSION="$2"; shift 2 ;;
    --version=*) VERSION="${1#--version=}"; shift ;;
    *) usage ;;
  esac
done

die() { printf '\033[1;31mx\033[0m %s\n' "$*" >&2; exit 1; }
say() { printf '\033[1;32m==>\033[0m %s\n' "$*"; }

command -v docker >/dev/null || die "docker is required"
docker buildx version >/dev/null 2>&1 || die "docker buildx is required"
command -v ssh >/dev/null && command -v scp >/dev/null || die "ssh and scp are required"

REPO="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO"
[ -f Dockerfile.receiver ] || die "Dockerfile.receiver not found in $REPO"

[ -n "$VERSION" ] || VERSION="$(git describe --always --dirty)"
COMMIT="$(git rev-parse --short HEAD)"
IMAGE=rfspager-receiver-build:arm64
BIN_DIR=/opt/rfspager
UNIT=rfspager-receiver

say "Building $VERSION ($COMMIT) for linux/arm64"
docker buildx build --platform linux/arm64 --target build -f Dockerfile.receiver \
  --build-arg "VERSION=$VERSION" --build-arg "COMMIT=$COMMIT" \
  -t "$IMAGE" --load .

say "Extracting the binary"
OUT="$(mktemp -d)"
trap 'rm -rf "$OUT"' EXIT
CID="$(docker create "$IMAGE")"
docker cp "$CID:/out/receiver" "$OUT/receiver"
docker rm "$CID" >/dev/null
[ -s "$OUT/receiver" ] || die "the build produced an empty binary"
ls -l "$OUT/receiver"

say "Checking access to $TARGET"
REMOTE_UID="$(ssh -o BatchMode=yes "$TARGET" 'id -u')" || die "cannot ssh to $TARGET"
SUDO=""
if [ "$REMOTE_UID" != "0" ]; then
  SUDO="sudo"
  ssh "$TARGET" 'sudo -n true' 2>/dev/null || die "the remote user needs passwordless sudo (or use root@)"
fi

say "Copying to $TARGET:/tmp/receiver.new"
scp -q "$OUT/receiver" "$TARGET:/tmp/receiver.new"

say "Installing and restarting $UNIT"
# shellcheck disable=SC2029
ssh "$TARGET" "set -e
  if [ -s $BIN_DIR/receiver ]; then $SUDO cp -p $BIN_DIR/receiver $BIN_DIR/receiver.prev; fi
  $SUDO install -m0755 -o root -g root /tmp/receiver.new $BIN_DIR/receiver
  rm -f /tmp/receiver.new
  $SUDO mkdir -p /var/lib/rfspager && $SUDO chmod 750 /var/lib/rfspager
  $SUDO systemctl restart $UNIT
  sleep 3
  echo \"service: \$(systemctl is-active $UNIT)\"
  $SUDO journalctl -u $UNIT -n 15 --no-pager"

say "Deployed $VERSION to $TARGET (rollback: $BIN_DIR/receiver.prev)"
