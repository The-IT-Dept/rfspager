# Receiver local UI

Every receiver serves a small web UI and JSON API on its own network so the
people hosting it can see what it is doing without an rfspager.app account:
live pages, decode and signal stats, upstream connections, logs and the
running settings. Hosts can also add webhooks that forward decoded pages to
their own systems.

- Served by the receiver binary itself (`internal/receiver/localui`), UI
  embedded with `go:embed` from `apps/receiver-ui` (Vite + React + Tailwind).
- Default listen address `:8148` (148 MHz). `RECEIVER_UI_ADDR` changes it;
  `RECEIVER_UI_ADDR=off` disables it.
- No login. Anyone who can reach the port can see everything and manage
  webhooks; access control is the host's network. Settings are read-only:
  tuning stays server-owned and is changed in the admin UI.
- Everything comes from the receiver itself, so it keeps working when the
  link to rfspager.app is down.

## Files on a receiver

| Path | What |
| --- | --- |
| `/var/lib/rfspager/pages.jsonl` | recent decoded pages (all decodes, flagged posted or filtered), rewritten to the newest 3000 when it grows past 6000 lines |
| `/var/lib/rfspager/webhooks.json` | the host's webhooks, secrets included (0600) |

Stats history (24 h) and logs (newest 2000 records) are in memory only and
start empty after a restart.

## Webhooks

A webhook receives every page the receiver forwards upstream, after the same
content filter (`post_all` on the server config bypasses it for both). Each
webhook has its own queue (256 pages) and worker, so a slow or dead webhook
never delays decoding or the upstream link; when a queue is full the oldest
page is dropped and counted as failed.

```
POST <url>
Content-Type: application/json
User-Agent: rfspager-receiver/<version>
X-RFSPager-Event: page            (or "test" from the Send test button)
X-RFSPager-Delivery: <uuid>       (same across retries of one delivery)
X-RFSPager-Signature: sha256=<hex HMAC-SHA256 of the raw body with the secret>   (only when a secret is set)

{
  "event": "page",
  "receiver": {"hostname": "rfspager-bud-1", "system": "Budgewoi", "version": "5.9.0-abc1234"},
  "page": {
    "id": 18231,
    "address": "0521839",
    "message": "RFS: GRASS FIRE ...",
    "time": "2026-10-16T14:02:31+11:00",
    "timestamp": 1792123351,
    "channel": "RFS",
    "freq_hz": 148587500,
    "baud": 512,
    "function": 3,
    "quality": {"codewords": 12, "corrected": 1, "uncorrectable": 0}
  }
}
```

Any 2xx is success. Anything else, or no answer within 10 s, is retried after
2 s, 10 s and 30 s, then counted as failed. Verifying the signature in a
receiving script:

```python
expected = "sha256=" + hmac.new(secret.encode(), body, hashlib.sha256).hexdigest()
ok = hmac.compare_digest(expected, request.headers["X-RFSPager-Signature"])
```

## API

All JSON. Times are unix milliseconds unless the field says otherwise. Lists
are newest first. Requests that change something (`POST`, `PUT`, `DELETE`)
must send `Content-Type: application/json` (with `{}` as the body when there
is nothing to send), otherwise they get 415. That forces a CORS preflight,
which the receiver never answers, so a web page on the internet cannot drive
the API through a visitor's browser.

### `GET /api/status`

Header pills and overview tiles.

```json
{
  "receiver": {"hostname": "rfspager-bud-1", "version": "5.9.0-abc1234", "os": "linux", "arch": "arm64",
               "started_at": 1792120000000, "host_uptime_s": 273600},
  "system": {"name": "Budgewoi", "uuid": "8947d028-..."},
  "dongle": {"open": true},
  "upstream": {"name": "prod", "url": "https://rfspager.app", "connected": true, "rtt_ms": 38},
  "webhooks": {"total": 2, "enabled": 2, "failing": 1},
  "pages_today": 412,
  "last_page_at": 1792123351000,
  "cpu_temp_c": 52.1,
  "undervoltage_now": false,
  "undervoltage_ever": true
}
```

`system` is null until the server has welcomed this receiver since it
started; `upstream` is null on a legacy-only receiver. `pages_today` counts
posted pages since local midnight; `last_page_at` is the newest posted page
(null when none).

### `GET /api/stats?window=1h|6h|24h`

```json
{"interval_s": 15, "latest": StatsPoint | null, "points": [StatsPoint, ...]}
```

`StatsPoint` is the `stats` frame the receiver sends upstream
(`rxproto.Stats`: `interval_s`, `dongle`, `host`, `link`, `channels[]`, see
`docs/receiver-management.md`) plus `"ts"`. `points` are oldest first, at most
720: longer windows are merged into buckets whose counters (`sync_locks`,
`cw_*`, `pages`, `pages_truncated`, `iq_buffers`, `iq_dropped`, `gaps`) are
summed and whose gauges (dBFS, SNR, carrier %, DSP load, temperature, RTT) are
averaged; `interval_s` is the length of a full bucket (the newest bucket may
be partial; each point's own `interval_s` is its real span). Stats are
collected even on a legacy-only receiver.

### `GET /api/pages?limit=100&before=<id>&filtered=1&q=<text>`

```json
{"pages": [Page, ...], "more": true}
```

```json
{"id": 18231, "ts": 1792123351000, "channel": "RFS", "freq_hz": 148587500, "baud": 512,
 "address": "0521839", "function": 3, "message": "RFS: GRASS FIRE ...", "posted": true,
 "quality": {"codewords": 12, "corrected": 1, "uncorrectable": 0, "truncated": false, "damaged": [[40, 44]]}}
```

`limit` defaults to 100 (max 500). `before` pages backwards by id.
`filtered=1` includes decodes the content filter dropped (`posted: false`:
noise, numeric beacons); default is posted pages only. `q` is a
case-insensitive substring match on address or message.

### `GET /api/logs?limit=500&level=debug|info|warn|error`

```json
{"logs": [{"id": 991, "ts": 1792123351000, "level": "info", "message": "page", "attrs": {"channel": "RFS", "address": "0521839"}}]}
```

`level` is the minimum level (default `debug`, i.e. everything kept).

### `GET /api/connections`

```json
{
  "endpoints": [{
    "name": "prod", "mode": "ws", "url": "https://rfspager.app", "primary": true,
    "connected": true, "connected_since": 1792120005000, "rtt_ms": 38,
    "reconnects_total": 0, "queue_depth": 0, "dropped_frames_total": 0,
    "pages_sent": 412, "pages_failed": 0, "last_page_at": 1792123351000,
    "last_error": "", "last_error_at": null
  }],
  "webhooks": [Webhook, ...]
}
```

For `ws` endpoints `pages_sent` counts pages queued on the socket and
`pages_failed` pages dropped because the queue was full. For `legacy`
endpoints `connected` is whether the last post succeeded and `rtt_ms`,
`reconnects_total`, `queue_depth`, `dropped_frames_total` are 0. Counters
start at zero on each restart.

### Webhooks

```json
Webhook = {
  "id": "c0b8...", "name": "Node-RED", "url": "http://192.168.1.20:1880/pages",
  "enabled": true, "has_secret": true, "created_at": 1792100000000,
  "state": "ok" | "failing" | "idle" | "disabled",
  "sent": 412, "failed": 0, "last_status": 200,
  "last_ok_at": 1792123351000, "last_error": "", "last_error_at": null
}
```

`state`: `disabled` when not enabled; `failing` as soon as a delivery
attempt fails (`failed` only counts pages whose retries are all used up);
`ok` after a success; `idle` before the first delivery. Counters start at zero on each restart.

- `GET /api/webhooks` → `{"webhooks": [Webhook]}`
- `POST /api/webhooks` `{"name", "url", "secret", "enabled"}` → 201 `Webhook`
- `PUT /api/webhooks/{id}` `{"name", "url", "enabled", "secret"?}` → `Webhook`.
  Leave `secret` out to keep the current one; `""` removes it.
- `DELETE /api/webhooks/{id}` → 204
- `POST /api/webhooks/{id}/test` `{}` → `{"ok": true, "status": 200, "error": "", "duration_ms": 41}`
  sends one `test` event straight away (no retries) with a sample page.

Validation failures are 422 `{"errors": {"url": "must be an http:// or https:// URL"}}`;
`name` is optional (≤ 60 chars), `url` must be absolute http(s), at most 20
webhooks. Unknown id is 404 `{"error": "..."}`.

### `GET /api/settings`

```json
{
  "config": RuntimeConfig, "config_version": 3, "config_source": "cache (/var/lib/rfspager/config.json)",
  "build": {"version": "5.9.0-abc1234", "os": "linux", "arch": "arm64", "hostname": "rfspager-bud-1"},
  "host": {"model": "Raspberry Pi 4 Model B Rev 1.5", "kernel": "6.1.0-rpi7-rpi-v8", "rtlsdr": "librtlsdr0 0.6.0-4"},
  "dongles": [{"index": 0, "name": "Generic RTL2832U OEM", "serial": "00000001", "tuner": "R828D"}],
  "endpoints": [{"name": "prod", "mode": "ws", "url": "https://rfspager.app", "primary": true}],
  "state_dir": "/var/lib/rfspager",
  "ui_listen": ":8148"
}
```

`RuntimeConfig` is `rxproto.RuntimeConfig` (gain in tenths of a dB, 0 = AGC;
`channels[]` of `{label, freq_hz, enabled}`). API keys are never returned.

### `GET /api/events`

Server-sent events for the live views:

| event | data |
| --- | --- |
| `page` | `Page` (every decode, posted or not; the UI filters) |
| `stats` | `StatsPoint`, each stats interval |
| `log` | log entry as in `/api/logs` |
| `status` | the `/api/status` body, every 5 s |

A `: ping` comment every 20 s keeps proxies from closing the stream. A slow
client is dropped rather than buffered.

### Anything else

`GET /` and any path not under `/api/` serves the embedded UI (unknown paths
get `index.html`, so client-side routes work on reload).

## Working on it

```sh
go run ./cmd/rxui-demo                 # API + embedded UI on :8148 with synthetic data,
                                       # webhook sink on :8149 (/hook ok, /fail 500, secret "demo")
cd apps/receiver-ui && npm run dev     # Vite on :5173, proxies /api to :8148
RX_UI_TARGET=http://192.168.15.52:8148 npm run dev   # or against a real receiver
```

`npm run build` writes the embedded files to `internal/receiver/localui/dist/`
(ignored by git apart from `.gitkeep`). `Dockerfile.receiver` builds the UI in
its own stage, so release binaries and `scripts/deploy-receiver.sh` builds
always carry it; a plain `go build` without it serves the API and a "built
without its web UI" page at `/`. `make receiver` builds the UI first.
