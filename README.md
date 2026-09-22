# RFSPager

Pager decoding, live incident feeds, and notifications for the NSW Rural Fire Service community. This repository contains the Go API and receiver, the React web application, and the Expo mobile application.

Licensed under **[GNU AGPLv3](LICENSE.md)** (`AGPL-3.0-only`). Preserve copyright and license notices. Covered derivatives distributed to others remain under AGPLv3; modified versions served over a network must offer their corresponding source to remote users. Commercial use is permitted subject to the license. Third-party components retain their own licenses; see [NOTICE](NOTICE) and [third-party notices](THIRD_PARTY_NOTICES.md).

## Deployment status

This is the replacement for the Laravel backend. Prepare and rehearse the migration before switching production traffic. The repository does not include the old application's operational data or credentials. Follow the [database migration procedure](docs/database-migration.md) and [cutover checklist](docs/cutover.md). Running the Go server starts importers and notification workers; use `cmd/migrate` when you intend only to change schema.

## Layout

- `cmd/server`: API, ingestion, importers, notification workers.
- `cmd/receiver`: RTL-SDR/POCSAG receiver using authenticated WebSocket transport.
- `cmd/migrate`: schema-only migration command.
- `internal`: Go services, embedded schema migrations, and tests.
- `apps/web`: React Router/Vite site and administration console.
- `apps/mobile`: Expo/React Native iOS and Android application.
- `docs`: development, integration, and deployment procedures.

## Local development

Use Go **1.27.1**, Node **22**, pnpm **10** for mobile, and npm for web. PostgreSQL **16** with PostGIS is required. Docker Compose provides a disposable loopback-only development database with no persistent volume:

```sh
docker compose up -d postgres
cp .env.example .env
# Set JWT_SECRET to the output of: openssl rand -hex 32
# Set DATABASE_URL to the local connection below.
# To create an administrator, explicitly set ADMIN_EMAIL and a strong ADMIN_PASSWORD.
export DATABASE_URL='postgres://postgres@127.0.0.1:55432/rfspager_test?sslmode=disable'
go run ./cmd/migrate
go run ./cmd/server
```

`cmd/server` reads `.env`; the schema-only migration command deliberately uses its explicit `DATABASE_URL` environment variable. No administrator is created unless bootstrap credentials are supplied. Bootstrap passwords must be 12–72 bytes. JWT secrets are required, must be independently generated, and must not be shared between installations.

In separate terminals:

```sh
cd apps/web
npm ci
npm run dev
```

```sh
cd apps/mobile
pnpm install --frozen-lockfile
# Configure your own Firebase/provider files and native development build first.
EXPO_PUBLIC_API_URL=http://localhost:8080 pnpm dev
```

The web development proxy forwards `/api` to `localhost:8080`. For a physical mobile device, set `EXPO_PUBLIC_API_URL` to a development server reachable from that device. Mobile defaults to loopback; forks never silently connect to production. See [mobile configuration](apps/mobile/README.md) and [environment reference](.env.example).

For mobile builds, supply your own Firebase config through `GOOGLE_SERVICE_INFO_PLIST` and `GOOGLE_SERVICES_JSON`, or their documented local files. Set `EXPO_PUBLIC_GOOGLE_MAPS_API_KEY`, `EXPO_PUBLIC_FACEBOOK_APP_ID`, `EXPO_PUBLIC_FACEBOOK_CLIENT_TOKEN`, `EXPO_PUBLIC_EAS_PROJECT_ID`, and `APPLE_TEAM_ID` as needed. Google Maps keys must be restricted to your application and APIs. Configure `EXPO_PUBLIC_API_URL` separately in each EAS build environment. App-store submission IDs, signing credentials, and provider accounts belong to the deployment, not this repository.

## Receivers

Provision an owner and approve a receiver in the administration UI. Each receiver gets its own generated key. Supply it through a protected environment file; don't put credentials in source, URLs, shared screenshots, or issue reports.

```sh
brew install librtlsdr    # macOS local builds
make receiver
RECEIVER_SERVER_URL=https://pager.example.org RECEIVER_API_KEY="$MY_RECEIVER_KEY" ./bin/receiver
```

Docker builds are available with `Dockerfile.receiver`; USB device access is needed when running the image. Receiver settings include `RECEIVER_SAMPLE_RATE`, `RECEIVER_TUNER_GAIN`, `RECEIVER_PPM`, `RECEIVER_DONGLE_INDEX`, `RECEIVER_DONGLE_SERIAL`, and `RECEIVER_DISABLE_SPECTRUM`.

The server accepts authenticated `POST /api/messages`, `GET /api/receiver/me`, and `GET /api/receiver/ws`. Only approved receivers with an active owner and key can authenticate. HTTPS is required except on loopback for local development. Legacy UUID-only ingress, Laravel Sanctum ingress, and receiver legacy backfeed have been removed. Old legacy environment variables fail startup explicitly.

## Authentication and billing

Admin and mobile JWTs have distinct audiences and token types. Credential changes and logout revoke sessions. The same admin checks protect streaming endpoints. Changing the JWT secret also invalidates issued sessions.

Password recovery is available after configuring SMTP; without it the endpoint explicitly reports unavailability. SMTP requires verified TLS, except for an explicit loopback development mode. The web application includes `/forgot-password` and `/reset-password`.

Stripe requires a webhook signing secret and configured price IDs. Apple and Google purchases are validated against deployment-specific product and environment allowlists and are bound to one account. Configure every legitimate existing subscription product ID before cutover. See [IAP webhook setup](docs/iap-webhooks.md).

Only explicitly trusted proxy CIDRs may supply forwarded client addresses to the authentication limiter. Configure `TRUSTED_PROXY_CIDRS` when using a reverse proxy, and use an additional shared edge limit for multiple API replicas. Application request logs omit query strings; configure your proxy/access-log sink to omit or redact credential-bearing query parameters too.

## Verification

```sh
go test ./... -count=1
go vet ./...
go build ./...
(cd apps/web && npm run typecheck && npm run build)
(cd apps/mobile && pnpm exec tsc --noEmit)
```

Database tests are opt-in because they reset a disposable schema:

```sh
export RFSPAGER_TEST_DATABASE_URL='postgres://postgres@127.0.0.1:55432/rfspager_test?sslmode=disable'
RFSPAGER_TEST_INSTALLATION_DATABASE_URL="$RFSPAGER_TEST_DATABASE_URL" \
  go test -p 1 ./internal/db ./internal/iap ./internal/mobile -count=1
docker compose down
```

Never supply a production connection to tests. CI runs code checks, dependency audits, and a redacted current-tree secret scan. Existing private Git history is not made safe by a green current-tree scan; complete the publication steps in [cutover.md](docs/cutover.md).

## Contributing and security

Community contributions are welcome. Start with [CONTRIBUTING.md](CONTRIBUTING.md); report vulnerabilities privately using [SECURITY.md](SECURITY.md). Use synthetic data in tests and issue reports. The app is a community information tool, not an official emergency dispatch or life-safety system.
