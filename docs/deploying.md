# Deploying to production

Production is the single box `apps01` (`root@apps01-syd-theitdept-au` over Tailscale; override with `RFSPAGER_PROD_HOST`):
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

## Store notifications

Subscription state comes straight from the stores.

- **Apple**: App Store Connect → RFSPager → App Information → App Store Server
  Notifications. There is no API for this field. Production Server URL and
  Sandbox Server URL are both `https://rfspager.app/api/webhooks/apple`
  (Version 2). The server verifies Apple's signature chain and the bundle ID.
  Verify without waiting for a renewal: build `cmd/apple-notify-test` for the
  box and run it from `/opt/rfspager-go`. Plain, it asks Apple for a TEST
  notification and prints each delivery attempt (`SUCCESS` once the handler
  answers 200). `-history 48h` lists what Apple actually sent and runs every
  signed payload through the webhook's verification; add `-replay <url>` to
  POST them to a webhook (the dev server's, to exercise the database path on
  the prod-data copy). `-print TEST` prints raw TEST payloads (no customer data),
  e.g. to refresh `internal/iap/testdata/apple_test_notification.jws`.
- **Free time**: the admin user page has a *Free time* card (superadmin). *Give
  free time* follows the account: an active Apple subscription gets its
  renewal date extended through the App Store Server API (at most 90 days per
  call, two calls per subscription in any 365 days, so up to 180 days), an
  active Google one is deferred through the Play API (up to 365 days), and
  everyone else gets `io.devlan.rfspager.gifted` in our own columns (up to
  730 days; the expiry sweep lapses it on time). Editing
  `subscription_expiry` by hand does nothing for an auto-renewing subscriber,
  because the store still charges on the original date. Store grants then
  arrive as `RENEWAL_EXTENDED` / `SUBSCRIPTION_DEFERRED` webhooks, which move
  the expiry. Every grant, failed ones included, is in `account_extensions`.
- **Supporters**: the admin system page has a *Supporters* card (superadmin).
  It lists people besides the owner who get free Pro while that receiver
  qualifies, on the same host comp rules. Add one by account email with a
  note; the host comp job starts their comp within 15 minutes and emails them
  in a thread of their own. If they pay through a store, they can cancel it
  and keep Pro while the receiver runs. Removing someone ends their comp
  straight away unless they own or support another receiver. Details are in
  `docs/plans/2026-10-14-public-coverage-and-hosting.md` (Supporters).
- **Google**: `scripts/google-rtdn-subscription.sh <topic>` adds a Pub/Sub push
  subscription on the RTDN topic named in Play Console, authenticated with an
  OIDC token the server checks (`GOOGLE_PUBSUB_AUDIENCE`,
  `GOOGLE_PUBSUB_SERVICE_EMAIL` in `.env`).
- `cmd/google-sub-check` (run on apps01 from `/opt/rfspager-go`) compares every
  live Google subscription with the Play API and prints mismatches; it never
  writes. To re-apply Google's state for one user, publish a RENEWED
  notification for their token to the RTDN topic (see the RTDN script's test
  command) and the webhook does the rest.
