# RFSPager

Real-time pager and incident feed for the NSW Rural Fire Service.

This is the monorepo for the **RFSPager** platform. It contains every piece
of the stack — the Go ingestion/API server, the iOS + Android mobile app,
the customer-facing web UI, and the CI/CD that ships them.

> **License.** RFSPager is source-available under [FSL-1.1-MIT](./LICENSE.md).
> Non-commercial use, internal use, education and research are all permitted;
> the license converts to plain MIT two years after each release.

---

## Repository layout

```
.
├── cmd/                 Go binaries
│   ├── server/          main API + ingestion service
│   ├── receiver/        RTL-SDR + POCSAG receiver agent (build tag: rtlsdr)
│   └── send-test/       small CLI for firing test pushes
├── internal/            Go server packages
│   ├── admin/           /api/v1/admin — admin JWT, CRUD
│   ├── mobile/          /api/v2/mobile — app REST + SSE
│   ├── iap/             Apple JWS + Google Play validation
│   ├── notify/          APNs + FCM dispatch
│   ├── importer/        RFS NSW HR + Major Incidents pollers
│   ├── stripe/          web checkout fallback
│   ├── db/              postgres + embedded migrations
│   ├── handler/         message ingress (HTTP)
│   ├── dispatch/        fan-out to subscribers
│   ├── middleware/      auth, logging
│   ├── models/          shared row types
│   ├── config/          env loading
│   └── public/          static assets served by Go
├── apps/
│   ├── mobile/          Expo SDK 56 / RN 0.85 app (iOS + Android)
│   └── web/             React Router 7 + Vite admin / customer UI
├── .github/workflows/   CI/CD (release-please, EAS, Docker)
├── docs/
│   ├── ci-secrets.md      how to get every cert/token CI needs
│   ├── iap-webhooks.md    App Store + Play Store notification setup
│   └── adapty-backfill.md one-off migration from Adapty
├── go.mod  go.sum       module github.com/the-it-dept/rfspager
├── .air.toml            live-reload config for the Go server
├── .env.example         every env var the Go server reads
├── LICENSE.md           FSL-1.1-MIT
└── laravel-old/         legacy reference, git-ignored, do not commit
```

---

## Quickstart

### Prerequisites

| Tool        | Version    | Used for                                 |
| ----------- | ---------- | ---------------------------------------- |
| Go          | 1.25+      | API server                               |
| Node        | 22.x       | mobile + web                             |
| pnpm        | 10.x       | both JS workspaces                       |
| PostgreSQL  | 16 +PostGIS| primary datastore                        |
| Docker      | latest     | building deploy images for server + web  |
| Xcode       | 16+        | iOS builds                               |
| Android SDK | 35         | Android builds                           |
| eas-cli     | latest     | mobile builds & store submission         |
| air         | latest     | optional, live-reload of Go server       |

### Boot every service locally

```bash
# 1. database
createdb rfspager
psql rfspager -c 'CREATE EXTENSION postgis;'

# 2. env
cp .env.example .env       # fill in DB url, JWT, push keys, IAP, etc.

# 3. Go API on :8080
air                        # or: go run ./cmd/server

# 4. web UI on :5173
cd apps/web && pnpm install && pnpm dev

# 5. mobile dev client (after `eas build --profile development`)
cd apps/mobile && pnpm install && pnpm dev
```

The web UI's Vite proxy forwards `/api/*` to `localhost:8080`, and the mobile
app talks to whatever `EXPO_PUBLIC_API_URL` resolves to for the active EAS
profile (see `apps/mobile/eas.json`).

---

## Components

### Go server (`./cmd/server`, `./internal`)

Single binary providing:

- **Message ingress** — `POST /api/messages/{system_id}`, `POST /api/v2/system/multimon_message`.
- **Admin API** — `/api/v1/admin/**`, JWT against the `admin_users` table.
- **Mobile API** — `/api/v2/mobile/**`, JWT against `users`. Includes REST
  resources, IAP validation, webhooks (Apple S2S, Google RTDN), and an SSE
  stream at `/api/v2/mobile/events`.
- **Importers** — background pollers that pull NSW RFS Hazard Reductions and
  Major Incidents every 5 minutes.
- **Notifications** — APNs (token auth, p8) and FCM via Firebase Admin.

Migrations are embedded (`internal/db/migrations`) and applied on boot. The
admin user is seeded from `ADMIN_EMAIL` / `ADMIN_PASSWORD` if absent.

### Mobile app (`./apps/mobile`)

Expo SDK 56 / React Native 0.85 / React 19. See [apps/mobile/README.md](./apps/mobile/README.md).

- Navigation: Expo Router with NativeTabs (iOS liquid-glass tab bar).
- State: Zustand (auth, preferences, subscriptions).
- Data: TanStack Query against the Go mobile API.
- IAP: `expo-iap` with server-side Apple JWS validation.
- Push: Firebase Messaging + Notifee.
- Bundle ID: `io.devlan.rfspager` on both platforms.

### Receiver agent (`./cmd/receiver`)

A standalone Go binary that drives RTL-SDR USB dongles, decodes POCSAG
pages off the air, and streams them up to the rfspager server over a
persistent WebSocket. Designed to run on Macs, Raspberry Pis, fanless
mini-PCs — anywhere you can plug a dongle.

- **Listens to multiple POCSAG frequencies through a single dongle** by
  channelising the wideband IQ stream in software (tune to the midpoint,
  shift each channel to baseband, lowpass + decimate, FM-demod, slice).
- Decodes POCSAG at 512, 1200 and 2400 baud in parallel — only the right
  baud locks, others drop out at sync detection.
- BCH(31,21) error correction; two-batch sync confirmation gives joint
  false-positive probability ≈ 6×10⁻¹⁷.
- Reports its identity (git sha, OS, arch, hostname, full dongle inventory
  with manufacturer/serial/tuner) to the server on connect — admin portal
  shows live online status and a real-time **spectrum waterfall**.
- Auto-reconnects on USB hot-unplug, network blips, and server restarts.
- Build tag `rtlsdr` gates the cgo bindings to librtlsdr, so the rest of
  the monorepo stays cgo-free.

#### Local build (macOS, via Homebrew)

```bash
brew install librtlsdr
make receiver        # builds bin/receiver via cgo + ldflags
make run-receiver    # also tees logs to pager.log
```

#### Build via Docker

The Docker image installs librtlsdr at build time and ships on Ubuntu 24.04
so it's identical to what you'd hand-install on a Pi 4 or a NUC.

```bash
docker build -f Dockerfile.receiver -t rfspager-receiver:dev .
```

Pull the published image (per tag below) from GHCR:

```bash
docker run --rm \
  --device /dev/bus/usb \
  -e RECEIVER_SERVER_URL=https://rfspager.example.com \
  -e RECEIVER_API_KEY=...    \
  ghcr.io/the-it-dept/rfspager-receiver:latest
```

`--device /dev/bus/usb` is required so the container can talk to the dongle.

#### Environment

| Variable                  | Default                  | Description                                              |
| ------------------------- | ------------------------ | -------------------------------------------------------- |
| `RECEIVER_SERVER_URL`     | `http://localhost:8080`  | Base URL of the rfspager server                          |
| `RECEIVER_API_KEY`        | *(required)*             | System's ingress key (from admin portal → Systems)       |
| `RECEIVER_SAMPLE_RATE`    | `1920000`                | RTL-SDR ADC sample rate (Hz)                             |
| `RECEIVER_TUNER_GAIN`     | `0` (AGC)                | Manual tuner gain in tenths of a dB; `496` ≈ 49.6 dB     |
| `RECEIVER_PPM`            | `0`                      | Crystal frequency correction (PPM)                       |
| `RECEIVER_DONGLE_INDEX`   | `-1` (auto)              | Pin to a specific dongle by enumeration index            |
| `RECEIVER_DONGLE_SERIAL`  | *(unset)*                | Pin to a specific dongle by USB serial (substring; wins over index) |
| `RECEIVER_DISABLE_SPECTRUM` | `0`                    | Set `1` to skip the waterfall FFT + WS spectrum frames   |
| `RECEIVER_LEGACY_URL`     | *(unset)*                | Enables **legacy mode** — POSTs pages to this base URL instead of using the WS transport |
| `RECEIVER_LEGACY_SYSTEM_UUID` | *(unset)*            | Required when `RECEIVER_LEGACY_URL` is set: the system's UUID in the legacy backend |

#### Legacy mode (backfeed to the old Laravel platform)

When `RECEIVER_LEGACY_URL` is set the receiver bypasses the WebSocket
transport entirely and POSTs every decoded page to the Laravel-era
endpoint at `{LEGACY_URL}/api/messages/{LEGACY_SYSTEM_UUID}`:

```
POST https://v3.rfspager.app/api/messages/<uuid>
Content-Type: application/json
{"address": "0521839", "message": "...", "timestamp": 1716691200}
```

No API key, no welcome handshake, no spectrum frames — the Laravel side
identifies the system by URL UUID alone and runs its own duplicate
detection (`address + md5` within ±5 min). Useful for keeping the old
platform fed while migration is in flight, or for running a single
receiver that feeds two production targets (start two processes pointing
at different upstreams).

```sh
RECEIVER_LEGACY_URL=https://v3.rfspager.app \
RECEIVER_LEGACY_SYSTEM_UUID=86c052af-2304-458f-92b3-99fd1ff75b13 \
  make run-receiver
```

Frequencies are currently hardcoded (148.5875 MHz RFS + 148.9625 MHz FRNSW)
in `cmd/receiver/main.go`. Lift to env when more systems come online.

### Web UI (`./apps/web`)

React Router 7 + Vite 7 + Tailwind 4 + shadcn/ui (base-ui flavour). See
[apps/web/README.md](./apps/web/README.md). Used both for the admin
console (`/admin/*`) and customer-facing pages.

---

## CI/CD

All workflows live in [`.github/workflows`](./.github/workflows/). They are
triggered by either pushes to `main` (release-please) or tags.

| Tag prefix         | Triggers                                                                     |
| ------------------ | ---------------------------------------------------------------------------- |
| `server-vX.Y.Z`    | Build & push Docker image `ghcr.io/the-it-dept/rfspager-server:X.Y.Z`        |
| `receiver-vX.Y.Z`  | Build & push Docker image `ghcr.io/the-it-dept/rfspager-receiver:X.Y.Z`      |
| `web-vX.Y.Z`       | Build & push Docker image `ghcr.io/the-it-dept/rfspager-web:X.Y.Z`           |
| `mobile-vX.Y.Z`    | EAS local build (iOS + Android) on a macOS runner, then submit to stores     |

EAS builds run **locally on the runner** (`eas build --local`) — they do not
queue against EAS cloud. Submission uses `eas submit`. See
[`docs/ci-secrets.md`](./docs/ci-secrets.md) for the full secret inventory and
how to mint each credential.

---

## Conventions

- **Module path:** `github.com/the-it-dept/rfspager` (server), npm package
  names are private (`"private": true`).
- **Branches:** `main` is the release branch; release-please opens PRs that
  bump per-component versions.
- **Tags:** component-scoped — `server-vX.Y.Z`, `web-vX.Y.Z`,
  `mobile-vX.Y.Z`. Pushing one of these to GitHub kicks off the matching
  release pipeline.
- **Secrets:** never committed. Local copies go in `./.env` and `./.secrets/`
  (both git-ignored). CI reads from GitHub Actions secrets — list in
  [`docs/ci-secrets.md`](./docs/ci-secrets.md).
- **Database:** migrations are embedded into the Go binary; never run them
  out-of-band.

---

## Contributing

Internal contributors only at this stage. The FSL license permits forks for
non-commercial use, but please open an issue before sending substantial
changes.
