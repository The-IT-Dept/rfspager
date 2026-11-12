# RFSPager — Web

The web UI for RFSPager. Hosts both the **admin console** (`/admin/*`) and
customer-facing pages. Part of the [RFSPager monorepo](../../README.md).

- **Framework:** React Router 7 (SSR) on Vite 7
- **UI:** Tailwind v4 + shadcn/ui (base-ui flavour)
- **Brand colour:** `#ff1003`, logo at `public/logo.svg`
- **API:** Vite dev-proxy forwards `/api/*` to the Go server on `:8080`

## Quickstart

```bash
npm ci
npm run dev       # http://localhost:5173

# Production build
npm run build
npm run start     # serves ./build via @react-router/serve
```

## Layout

```
app/
├── routes/        file-based routes (admin/*, public)
├── components/    shadcn UI + custom
├── lib/           shared helpers
└── root.tsx       app shell
```

Admin login lives at `/admin/login`; the Go server's admin JWT is kept in
`localStorage` and sent as a bearer token.

## Docker

A multi-stage `Dockerfile` is included. CI builds and pushes
`ghcr.io/the-it-dept/rfspager-web:<tag>` on `vX.Y.Z` release tags.

```bash
docker build -t rfspager-web .
docker run -p 3000:3000 rfspager-web
```

## Notes

- The shadcn sidebar in this repo uses the **base-ui** variant — pass a
  `render` prop (not `asChild`).
- Routes under `/admin` require a session; everything else is public.
- The previous name was `admin/`; renamed to `ui/` then `apps/web/` so the
  same SSR app can host marketing/customer pages alongside admin.
