#!/usr/bin/env bash
# Build the web UI image for apps01 from a git ref, ship it and switch the container.
#
#   scripts/deploy-web.sh [ref] [ssh-target]
#
# Defaults to origin/main and root@100.64.244.122 (Tailscale). apps01 has no
# Node and GHCR packages are private, so the arm64 image is built here and
# sent with `docker save | docker load`. The image is tagged
# rfspager-web:<ref>-<sha>; the previous image stays loaded for a rollback by
# hand (`docker run` it with the same arguments).
set -euo pipefail

REF="${1:-origin/main}"
TARGET="${2:-root@100.64.244.122}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

git fetch -q origin
SHA="$(git rev-parse --short "$REF")"
IMAGE="rfspager-web:${REF##*/}-$SHA"
CTX="$(mktemp -d)"
trap 'rm -rf "$CTX"' EXIT
git archive --format=tar "$REF" apps/web | tar -x -C "$CTX"

docker build --platform linux/arm64 -t "$IMAGE" "$CTX/apps/web"
echo "built $IMAGE from $REF ($SHA)"
docker save "$IMAGE" | ssh "$TARGET" docker load

ssh "$TARGET" "IMAGE=$IMAGE bash -s" <<'REMOTE'
set -euo pipefail
docker image inspect "$IMAGE" >/dev/null
docker rm -f rfspager-web >/dev/null 2>&1 || true
docker run -d --name rfspager-web --network host --restart unless-stopped \
  -e NODE_ENV=production -e HOST=127.0.0.1 -e PORT=3100 -e API_URL=http://127.0.0.1:8080 \
  "$IMAGE" >/dev/null
for i in $(seq 1 20); do
  code="$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:3100/admin/login || true)"
  if [ "$code" = "200" ]; then
    echo "web HTTP 200 after ${i}s"
    break
  fi
  sleep 1
done
[ "${code:-}" = "200" ] || { echo "web did not come up (HTTP ${code:-none})"; docker logs --tail 20 rfspager-web; exit 1; }
docker ps --filter name=rfspager-web --format '{{.Names}} {{.Image}} {{.Status}}'
REMOTE
