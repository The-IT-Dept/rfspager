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
├── cmd/                 Go server entry points
│   ├── server/          main API + ingestion service
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
│   └── ci-secrets.md    how to get every cert/token CI needs
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

### Web UI (`./apps/web`)

React Router 7 + Vite 7 + Tailwind 4 + shadcn/ui (base-ui flavour). See
[apps/web/README.md](./apps/web/README.md). Used both for the admin
console (`/admin/*`) and customer-facing pages.

---

## CI/CD

All workflows live in [`.github/workflows`](./.github/workflows/). They are
triggered by either pushes to `main` (release-please) or tags.

| Tag prefix       | Triggers                                                                   |
| ---------------- | -------------------------------------------------------------------------- |
| `server-vX.Y.Z`  | Build & push Docker image `ghcr.io/the-it-dept/rfspager-server:X.Y.Z`      |
| `web-vX.Y.Z`     | Build & push Docker image `ghcr.io/the-it-dept/rfspager-web:X.Y.Z`         |
| `mobile-vX.Y.Z`  | EAS local build (iOS + Android) on a macOS runner, then submit to stores   |

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
