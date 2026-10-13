# Receiver remote management: design and build plan

Date: 2026-10-13. One document, built in one pass.

## Why

Newcastle (prod system 3) decodes the RFS channel on 148.5875 MHz badly while FRNSW on 148.9625 MHz is perfect. The Pi is not reachable, so nothing can be measured or tuned. Today every tuning change is an env-file edit over SSH and the only upgrade path is re-running the installer. We want to diagnose and tune every receiver from the admin UI, consistently, push updates, and keep the server as the source of truth.

## Decisions

- The server is authoritative for receiver tuning. Hosts (system owners) can view settings and health. Only admins change them.
- Narrow typed command set. No arbitrary shell.
- Backward compatible with receivers 5.2.x and 5.3.x in the field. They ignore unknown frames.
- On-demand captures, not a continuous waterfall (the waterfall was removed in September for cost).
- Validate on Budgewoi and Figtree, cut a release, then Newcastle. The Newcastle box runs 5.3.0 which cannot self-update, so its host re-runs the one-line installer once. After that it is fleet-managed.

## Non-goals

Mobile app screens for owners (API only). Multi-dongle receivers. Fleet-wide bulk update UI. Averaged stats history (sampled instead). Spectrum streaming.

## Architecture

```
Receiver  <-- websocket JSON frames -->  Go server (recvhub)  <-- pg_notify + tables -->  Postgres
Admin UI  <-- REST + SSE, admin JWT -->  Go server (admin API)
Owner app <-- REST, mobile JWT      -->  read-only config
```

### Shared protocol package

`internal/rxproto` holds the payload types used by both sides (Hello, Welcome, Page, Log, Stats, Config, ConfigResult, Command, CommandResult) plus `RuntimeConfig` with `Defaults()` and `Validate()`. `recvhub` and `wsclient` import it instead of keeping duplicate structs.

### Frames

Receiver to server:

- `hello`: existing fields plus `proto: 2`, `host {model, kernel, uptime_s, rtlsdr}`, `config {version, effective}` where `effective` is the RuntimeConfig the receiver is running.
- `page`, `log`, `ping`: unchanged.
- `stats`: StatsPayload, every `stats_interval_s`.
- `config_result`: `{version, status: applied|rejected, errors {key: message}, effective}`.
- `command_result`: `{id, type, status: progress|ok|error, stage, message}`.

Server to receiver:

- `welcome`: existing fields plus `config {version, config}` and `latest_version`.
- `config`: `{version, config}`.
- `command`: `{id, type, params}`.
- `pong`, `ack`: unchanged.

The server sends `config` and `command` only to connections with `proto >= 2`.

### RuntimeConfig

```json
{
  "tuner_gain_tenths_db": 0,
  "ppm": 0,
  "sample_rate": 1920000,
  "dongle_serial": "",
  "dongle_index": -1,
  "post_all": false,
  "log_level": "info",
  "stats_interval_s": 15,
  "channels": [
    {"label": "RFS",   "freq_hz": 148587500, "enabled": true},
    {"label": "FRNSW", "freq_hz": 148962500, "enabled": true}
  ]
}
```

Defaults reproduce today's hardcoded behaviour, so a system with no revisions behaves exactly as now. Gain 0 means AGC.

Validation, shared by both sides: gain 0..600; ppm -200..200; sample_rate a multiple of 38400 within 768000..2400000; 1..4 channels with unique non-empty labels, at least one enabled, enabled span at most 0.8 x sample_rate; log_level in debug|info|warn; stats_interval_s 5..300; dongle_index >= -1.

Precedence on the receiver: server revision, then the on-disk cache at `/var/lib/rfspager/config.json`, then env bootstrap values (`RECEIVER_TUNER_GAIN` and friends), which are only honoured when neither of the first two exists.

Seeding: when a proto-2 receiver connects and its system has no revision yet, the server stores revision 1 from `hello.config.effective` with source `seeded-from-receiver`. Figtree's fixed-gain trial migrates by itself.

Apply on the receiver: gain, ppm, log_level, stats_interval_s and post_all apply live. sample_rate, dongle selection and channels restart the dongle loop. Channels are built on each dongle open, with the centre at the midpoint of the enabled channels. The outcome goes back as `config_result`; the server stores `config_version` and `config_status` on `system_connections` and writes a system event.

### Stats

Counters are deltas for the interval unless named `_total`.

```
dongle   {open, gain_tenths_db, agc, ppm, centre_hz, sample_rate, iq_buffers, iq_dropped, gaps, dsp_load}
host     {cpu_temp_c, load1, mem_free_mb, throttled_hex, undervoltage_now, undervoltage_ever, uptime_s}
link     {reconnects_total, queue_depth, dropped_frames_total, rtt_ms}
channels [{label, freq_hz, power_dbfs, noise_floor_dbfs, snr_db, carrier_pct,
           sync_locks, sync_losses, cw_clean, cw_corrected, cw_uncorrectable,
           pages, pages_truncated, last_page_age_s}]
```

Measurement:

- Per channel, block power after the FIR decimator goes into a 1 dB histogram. p10 is the noise floor, p90 is the signal power, carrier_pct is the share of blocks more than 6 dB above the floor.
- The decoder keeps atomic counters: sync locks and losses, codeword outcomes from `correctBCH` (clean, corrected, uncorrectable), pages emitted, and pages truncated (a flush caused by resync or sync loss while message bits were staged).
- dsp_load is time spent in channel processing divided by buffer duration.
- Host facts come from sysfs and procfs: thermal zone, loadavg, meminfo, uptime, and the Pi firmware `get_throttled` value (bit 0 undervoltage now, bit 16 undervoltage since boot).
- rtt from the ping/pong round trip.

Server side: insert into `system_stats`, update `system_connections.last_stats` and `last_stats_at`, fan out through a StatsBroker shaped like the LogBroker. Retention 30 days, pruned by the hub sweeper.

### Commands

Rows in `system_commands`. A trigger raises `pg_notify('receiver_command', id)` on insert. The hub sends to a live proto-2 connection and marks the row `sent`. Queued commands are delivered on connect. Commands expire after one hour. Results update status, stage and message and write system events.

- `restart`: reply ok, exit 0. systemd restarts the unit.
- `dongle_reset`: cancel the dongle context. The outer loop reopens.
- `update {version}`: download `/api/binaries/receiver/{platform}?version=vX.Y.Z` and its `.sha256` with the apikey, verify, write `receiver.new`, rename the current binary to `receiver.prev` and the new one into place, write `/var/lib/rfspager/update.json` with `{command_id, from, to, started_at, boots: 0}`, report progress stages, exit 0. On start, if the marker is present and the running version equals `to`, increment `boots`; more than 3 boots within 10 minutes swaps `receiver.prev` back and exits. On the first welcome the marker is removed and the recorded command gets an ok result. A marker for a different version is removed as stale.
- `capture {capture_id, kind, channel, seconds}` with seconds at most 30. `spectrum` is a 1024-bin averaged power spectrum of the raw IQ over one second as JSON `{centre_hz, sample_rate, bins_db}`. `audio` is the FM-demodulated output of one channel at the audio rate as int16 little-endian. `iq` is the post-FIR complex baseband of one channel as interleaved int16 little-endian. The receiver uploads with `PUT /api/receiver/captures/{id}` using the apikey, `application/octet-stream`, and `X-Capture-Kind`, `X-Capture-Rate`, `X-Capture-Channel` headers. The server stores the file under `CAPTURE_DIR/{system_id}/{id}` with a 64 MB cap and marks the row ready.

`cmd/replay` runs an audio or iq capture through the same slicers and decoders and prints the pages and decoder counters, so DSP changes can be tested against real Newcastle signal on a laptop.

### Server data model

One new migration:

- `receiver_config_revisions (id, system_id, version, config jsonb, source, note, created_at, unique (system_id, version))` with an insert trigger raising `receiver_config` with the system id.
- `system_connections` gains `proto int default 1`, `config_version int`, `config_status text`, `host jsonb`, `last_stats jsonb`, `last_stats_at timestamptz`.
- `system_stats (system_id, ts, stats jsonb, primary key (system_id, ts))`.
- `system_commands (id uuid, system_id, type, params jsonb, requested_by, status, stage, message, created_at, updated_at)` with status in queued|sent|progress|ok|error|expired and an insert trigger raising `receiver_command` with the id.
- `system_captures (id uuid, system_id, command_id, kind, channel, seconds, sample_rate, status, path, size_bytes, error, requested_by, created_at, ready_at)` with status in requested|recording|ready|error.

Server env: `CAPTURE_DIR`, default `./captures`, prod `/var/lib/rfspager-go/captures`.

### Admin API

Under `/api/v1/admin` with the admin JWT. The two SSE routes also accept `?token=` because EventSource cannot set headers.

```
GET  /systems/{id}/config                   current, defaults, last 20 revisions, applied {version, status}
PUT  /systems/{id}/config                   validate, insert revision, return current
GET  /systems/{id}/stats?since=6h&step=1m   sampled rows [{ts, stats}]
GET  /systems/{id}/stats/stream             SSE, live stats
GET  /systems/{id}/logs/recent?limit=       shared with the owner endpoints
GET  /systems/{id}/logs/stream              SSE
GET  /systems/{id}/events?limit=
POST /systems/{id}/commands {type, params}  returns the row
GET  /systems/{id}/commands?limit=
GET  /systems/{id}/captures
GET  /systems/{id}/captures/{cid}/download
GET  /receivers/latest-version              from GitHub, cached 5 minutes
```

Owner, mobile JWT: `GET /api/v2/mobile/systems/{uuid}/config` returns the current config read-only.

The history handlers move to `internal/recvhub/history.go` and take a system-id resolver, so the owner and admin routes share one implementation.

### Admin UI

The system detail page becomes tabbed.

- Overview: existing cards plus live health tiles from the stats stream: per-channel SNR, carrier share, uncorrectable share and pages; dongle gain, overflows and DSP load; host temperature, throttling and undervoltage; link reconnects and queue.
- Config: a form for RuntimeConfig. Gain as the R828D dB ladder plus AGC, ppm, sample rate, dongle, post-all, log level, stats interval, and an editable channels table. Save, applied-or-rejected status, revision history.
- Logs and events: live log viewer with level filter and pause, plus the events list.
- Diagnostics: 6h and 24h charts of SNR and uncorrectable rate per channel from sampled stats, a spectrum snapshot button with its chart, and capture request plus list with download.
- Update: current versus latest version, update to latest or to a given version, restart, dongle reset, command history.

Charts are small inline SVG components with no new dependencies, styled per the dataviz skill.

### Receiver packages

- `internal/rxproto`: shared types and validation.
- `internal/receiver/stats`: collector and host facts.
- `internal/receiver/control`: config apply and persistence, command handling, self-update, captures.
- Counters and capture taps in `pipeline.go`, `pocsag`, and `sdr`.
- `cmd/receiver`: wiring. `cmd/replay`: offline decode of captures.
- Installer: creates `/var/lib/rfspager`. Otherwise unchanged.
- `scripts/deploy-receiver.sh <host>`: the arm64 docker build, scp, install and restart recipe that so far lived only in notes. `docs/receiver-management.md`: runbook.

### Testing

Go unit tests: RuntimeConfig validation; decoder counters using `pocsag/encode` with injected bit errors; power histogram percentiles; update swap and rollback marker on a temp dir; command queue and config seeding (database tests, need `RFSPAGER_TEST_DATABASE_URL`); admin config endpoint validation. Web: typecheck and build.

Field validation on Budgewoi, then Figtree: stats visible in the UI; a gain change applies live and shows in the next stats; spectrum and audio captures download and replay decodes them; the update command from the pre-release build to the released build succeeds and the marker clears.

### Rollout

1. Merge. Deploy the server from main to apps01. Old receivers keep working.
2. Build the arm64 receiver from main. Deploy to Budgewoi, verify. Deploy to Figtree, verify.
3. Merge the release-please PR so the receiver release workflow publishes assets.
4. Send `update` for the new version to Budgewoi and Figtree as the real self-update test.
5. The Newcastle host runs the installer one-liner shown on the admin system page.
6. Tune Newcastle from the UI: read RFS SNR and carrier share, trial gains, capture audio and IQ for replay.
7. Deploy the web UI container.

### Build order

1. rxproto with RuntimeConfig and validation, with tests.
2. Migration and db functions.
3. Hub: proto-2 hello, welcome with config, seeding, stats, config_result and command_result handling, StatsBroker, command delivery, capture upload, shared history handlers.
4. Install proxy version parameter and latest-version endpoint.
5. Admin API and SSE token auth.
6. Receiver: config apply and persistence, counters, stats collector, control, main wiring.
7. cmd/replay.
8. Admin UI tabs.
9. Scripts and runbook.
10. Field validation and release.
