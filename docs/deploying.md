# Deploying to production

Production is the single box `apps01` (`root@100.64.244.122` over Tailscale):
the Go server as the `rfspager-go` systemd unit and the web UI as the
`rfspager-web` Docker container behind Caddy. Receivers update themselves
from GitHub releases; see `docs/receiver-management.md` for those.

Both scripts build from a git ref (default `origin/main`), never from the
working tree, so merge first and then deploy.

```sh
scripts/deploy-server.sh            # Go server from origin/main
scripts/deploy-web.sh               # web UI image from origin/main
scripts/deploy-server.sh v5.6.1     # or any tag / branch / sha
```

You need Go, Docker (for the arm64 web image) and ssh access to apps01.

## What they do

`deploy-server.sh` cross-compiles `cmd/server` for linux/arm64, copies it
up, keeps the running binary as `/opt/rfspager-go/server.prev`, restarts the
unit and polls `/health` for up to 20 seconds. The old process drains SSE
and websocket workers before the port answers again, so a single early probe
would wrongly report a failure. Migrations run on boot; the script prints the
latest applied version.

`deploy-web.sh` builds `apps/web` into `rfspager-web:<ref>-<sha>`, ships it
with `docker save | docker load` (no Node on the box, GHCR is private) and
replaces the container with the same run arguments Caddy expects
(`--network host`, port 3100, API on 8080).

## Rolling back

Server: `install -m 755 /opt/rfspager-go/server.prev /opt/rfspager-go/server && systemctl restart rfspager-go`.

Web: `docker images rfspager-web` lists the previous tags; rerun the
`docker run` line from `deploy-web.sh` with the old tag.

## Legacy GraphQL (old 4.x app)

Laravel is retired. `POST /graphql` on `v3.rfspager.app` is routed by Caddy to
the Go server, where `internal/legacy` answers every operation with fixed data
that puts a single "update required" notice at the top of the old app's feed.
It never returns a GraphQL error (the old app cannot display one) and never
touches the database. Remove the route once no 4.x installation has been seen
for a while (`installations.app_version`).
