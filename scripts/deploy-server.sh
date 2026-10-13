#!/usr/bin/env bash
# Build the Go server from a git ref and deploy it to apps01.
#
#   scripts/deploy-server.sh [ref] [ssh-target]
#
# Defaults to origin/main and root@100.64.244.122 (Tailscale). The build is
# taken from `git archive` of the ref, never from the working tree, so what
# ships is exactly what is on the branch. The previous binary is kept as
# /opt/rfspager-go/server.prev for a rollback by hand.
set -euo pipefail

REF="${1:-origin/main}"
TARGET="${2:-root@100.64.244.122}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

git fetch -q origin
SHA="$(git rev-parse --short "$REF")"
CTX="$(mktemp -d)"
trap 'rm -rf "$CTX"' EXIT
git archive --format=tar "$REF" | tar -x -C "$CTX"
(
  cd "$CTX"
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath \
    -ldflags="-s -w -X main.version=${REF##*/}-$SHA -X main.commit=$SHA" \
    -o "$CTX/server.arm64" ./cmd/server
)
echo "built server from $REF ($SHA)"

scp -q "$CTX/server.arm64" "$TARGET:/tmp/server.new"
ssh "$TARGET" 'bash -s' <<'REMOTE'
set -euo pipefail
cp -p /opt/rfspager-go/server /opt/rfspager-go/server.prev
install -m 755 /tmp/server.new /opt/rfspager-go/server
systemctl restart rfspager-go
# The old process drains SSE and websocket workers before the port is
# answered again; poll rather than probe once.
for i in $(seq 1 20); do
  code="$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/health || true)"
  if [ "$code" = "200" ]; then
    echo "health HTTP 200 after ${i}s"
    break
  fi
  sleep 1
done
[ "${code:-}" = "200" ] || { echo "health check failed (HTTP ${code:-none})"; exit 1; }
systemctl is-active rfspager-go
sudo -u postgres psql -d rfspager -Atc "select 'migration ' || version from schema_migrations order by version desc limit 1"
REMOTE
