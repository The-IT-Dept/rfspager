# Receiver remote management (receiver side)

How a Pi receiver takes its tuning from the server, reports health, runs
commands, updates itself and rolls back. The server/admin side is in
`docs/plans/2026-10-13-receiver-remote-management.md`; this is the view from
the receiver and what to do when operating one.

## Files on a receiver

| Path | What |
| --- | --- |
| `/opt/rfspager/receiver` | the binary (systemd unit `rfspager-receiver`, `Restart=always`) |
| `/opt/rfspager/receiver.prev` | the previous binary, kept by the installer, the deploy script and self-update |
| `/etc/rfspager/receiver.env` | bootstrap: `RECEIVER_SERVER_URL`, `RECEIVER_API_KEY`, optional `RECEIVER_*` tuning |
| `/etc/rfspager/endpoints.json` | optional fan-out; the first `ws` endpoint is the primary |
| `/var/lib/rfspager/config.json` | cached server config: `{"version", "config", "saved_at"}` |
| `/var/lib/rfspager/update.json` | self-update marker: `{"command_id", "from", "to", "started_at", "boots"}` |
| `/var/lib/rfspager/pages.jsonl`, `webhooks.json` | the local UI's page history and the host's webhooks, see `docs/receiver-local-ui.md` |

The state dir is `/var/lib/rfspager` (override with `RECEIVER_STATE_DIR`; tests
and dev checkouts use a temp dir). The receiver creates it (0750) on start and
the installer creates it too. If it cannot be created the receiver logs a
warning and runs without persistence: config still applies, self-update is
refused (no rollback marker possible).

## Config precedence

The server owns the tuning (`rxproto.RuntimeConfig`: gain, ppm, sample rate,
dongle, post_all, log level, stats interval, channels). On start the receiver
runs, in order:

1. the cached server revision in `config.json`, if it parses and validates;
2. else the env bootstrap: `RECEIVER_TUNER_GAIN`, `RECEIVER_PPM`,
   `RECEIVER_SAMPLE_RATE`, `RECEIVER_DONGLE_INDEX`, `RECEIVER_DONGLE_SERIAL`,
   `RECEIVER_POST_ALL` laid over the defaults, as version 0;
3. else (the env values do not validate, e.g. a sample rate that is not a
   multiple of 38400) the built-in defaults, with an error in the log.

The `hello` frame carries `config {version, effective}`; a system with no
revision is seeded on the server from it, so a Figtree-style fixed-gain env
setting migrates by itself. Every `welcome` and `config` frame from the
primary endpoint is validated, applied, written to `config.json` and answered
with `config_result {version, status: applied|rejected, errors, effective}`.
Once a revision is cached the env tuning variables are ignored for good;
change tuning in the admin UI.

Live (no interruption): tuner gain, ppm, log level, stats interval,
post_all. Everything else (sample rate, dongle index/serial, channels)
restarts the dongle loop: the device is closed and reopened with the new
config and the channels are rebuilt with the centre at the midpoint of the
enabled channels. A gain or ppm the dongle refuses falls back to the same
reopen. `config_result` says `applied` as soon as the config is stored; if the
reopen then fails (bad serial, unplugged dongle) stats show `dongle.open:
false` and the loop keeps retrying.

A receiver that never hears from the server (legacy-only, or an old server)
behaves exactly as before: defaults, two hardcoded channels, AGC.

## Endpoints

With `endpoints.json`, the first `ws` endpoint is the primary. Only it gets a
protocol-2 hello (host facts, running config), stats, config/command results
and shipped logs, and only its `config` and `command` frames are honoured
(frames from a secondary are ignored at debug level). Secondary ws endpoints
and legacy endpoints receive pages only, exactly as before, so a dev server
running older code never sees a frame it does not know. Self-update downloads
and capture uploads use the primary endpoint's URL and key.

## Stats

Every `stats_interval_s` (default 15 s) the receiver sends one `stats` frame:

- `dongle`: open, gain/AGC, ppm, centre, sample rate, IQ buffers processed,
  buffers dropped, gaps (deltas), and `dsp_load` = time in channel processing
  / real-time duration of the IQ (1.0 = only just keeping up).
- `host`: CPU temperature, load1, MemAvailable, uptime, and the Pi firmware
  `get_throttled` value (bit 0 undervoltage now, bit 16 since boot). All zero
  / empty where the files do not exist (a Mac).
- `link`: reconnects, send-queue depth, dropped frames, last ping RTT.
- `channels[]`: per channel, from a 1 dB histogram of block power after the
  channel filter: `noise_floor_dbfs` (p10), `power_dbfs` (p90), `snr_db`
  (p90 - p10), `carrier_pct` (blocks more than 6 dB above the floor);
  decoder deltas `sync_locks`, `sync_losses`, `cw_clean`, `cw_corrected`
  (1-2 bit BCH repairs), `cw_uncorrectable`, `pages`, `pages_truncated`
  (a page cut short by a stream gap or lost sync); `last_page_age_s` (-1 =
  none yet).

Reading them: a healthy channel idles with `carrier_pct` near 0 and jumps
during bursts; a high `cw_uncorrectable` share with good SNR points at
deviation/filter problems rather than signal level; any `iq_dropped` or
`dsp_load` near 1 means the Pi is falling behind.

## Commands

Delivered as `command {id, type, params}`; the receiver answers with
`command_result {id, type, status: progress|ok|error, stage, message}`.

- `restart`: replies ok, drains the socket (2 s grace) and exits 0; systemd
  restarts the unit.
- `dongle_reset`: closes and reopens the dongle (the outer loop reopens
  immediately, no backoff).
- `update {version}`: see below.
- `capture {capture_id, kind, channel, seconds}`: see below.

One update or capture runs at a time; a second is answered with an error.

## Self-update and rollback

`update {version: "vX.Y.Z"}`:

1. `downloading`: `GET <primary>/api/binaries/receiver/<platform>?version=vX.Y.Z`
   (platform `linux-arm64`, `linux-amd64` or `linux-armv7`, as install.sh)
   streamed to `/opt/rfspager/receiver.new` (0755).
2. `verifying`: `GET ...<platform>.sha256?version=vX.Y.Z`, first field compared
   with the download's sha256. Mismatch or a missing checksum aborts and
   removes `receiver.new`.
3. `swapping`: `receiver` -> `receiver.prev`, `receiver.new` -> `receiver`;
   `update.json` written with `boots: 0`.
4. `restarting`: exit 0, systemd starts the new binary.

On every start, before dialling: if `update.json` exists and its `to` matches
the running version (compare `vX.Y.Z` with `X.Y.Z-<commit>` after stripping
`v` and anything from the first `-`), `boots` goes up. More than 3 boots
inside 10 minutes of `started_at` means the new binary cannot stay up:
`receiver.prev` is renamed back over `receiver`, the marker is deleted, an
error is logged and the process exits 1 so systemd starts the restored
binary. A marker for a different version is deleted as stale. After the first
welcome the marker is deleted and the recorded command gets
`{status: ok, stage: running, message: "now running X.Y.Z-..."}`.

Note the boot counter counts process starts, not crashes: a receiver that
exits because the server is unreachable (no welcome within 15 s) three times
right after an update will roll back too. Confirm the server is up before
sending `update`.

Manual rollback: `sudo mv /opt/rfspager/receiver.prev /opt/rfspager/receiver
&& sudo systemctl restart rfspager-receiver`.

## Captures

`capture {capture_id, kind, channel, seconds}` records, reports
`progress: recording` then `progress: uploading`, and uploads with
`PUT <primary>/api/receiver/captures/<capture_id>` (headers `apikey`,
`X-Capture-Kind`, `X-Capture-Rate`, `X-Capture-Channel`).

- `spectrum`: one averaged 1024-bin power spectrum of the raw dongle IQ over
  ~1 s (Hann window, |X|^2 averaged, normalised so a full-scale tone reads 0
  dB), as `application/json` `{centre_hz, sample_rate, bins_db[1024]}` with
  bins from centre - fs/2 to centre + fs/2. Rate header = dongle sample rate.
- `audio`: `seconds` (1..30, default 10) of the named channel's FM demod
  output at 38400 Hz, int16 little-endian, 32767 = pi rad/sample.
- `iq`: the named channel's complex baseband after the channel filter at
  38400 Hz, interleaved int16 little-endian I,Q, 32767 = 1.0 (fixed scale,
  the same reference as the dBFS stats; not normalised to the signal).

The channel is the config label (`RFS`, `FRNSW`). A capture on a channel whose
dongle stops delivering samples times out and reports an error.

### Replaying a capture

```sh
go run ./cmd/replay -kind audio -rate 38400 newcastle-rfs.audio
go run ./cmd/replay -kind iq    -rate 38400 newcastle-rfs.iq
```

It runs the file through the receiver's own slicers and 512/1200/2400 baud
decoders (`receiver.NewDecoders`), prints one line per page
(`address fn baud "message"`) and the decoder counters at the end. Change
the DSP, run it again, compare counters.

## Deploying a build by hand

```sh
scripts/deploy-receiver.sh rfspager@192.168.15.52            # Budgewoi
scripts/deploy-receiver.sh root@100.79.78.79 --version v5.5.0-rc1
```

Builds linux/arm64 with `Dockerfile.receiver` (same toolchain and Debian 12
ABI as the release), extracts `/out/receiver`, copies it to the target,
keeps the old binary as `receiver.prev`, installs the new one, restarts the
unit and prints `systemctl is-active` plus the last 15 journal lines. The
remote user may be root or a sudoer. The version baked in defaults to
`git describe --always --dirty`.

## Bringing a 5.3.0 receiver into the fleet

Receivers on 5.2.x/5.3.x speak protocol 1: they keep working but cannot be
configured, report no stats and cannot self-update. One manual step: on the
admin system page copy the installer one-liner and run it on the box
(`curl -fsSL https://rfspager.app/api/install | sudo bash -s -- <key>`). The
installer keeps the existing tuning variables, creates `/var/lib/rfspager` and
restarts the unit. From then on the receiver is fleet-managed: config from
the admin UI, stats and captures, `update` for every later release.

## Troubleshooting

- `runtime config source=defaults ... problems=...`: the `RECEIVER_*` env
  values do not validate; fix or remove them, or push a revision from the UI
  (it will be cached and the env ignored).
- `config revision rejected`: the server sent something that fails the shared
  validation (`rxproto.Validate`); the UI shows the field errors.
- `UPDATE ROLLED BACK`: the new binary restarted too often; the previous one
  is running again and the update command shows an error on the server.
- `state dir unavailable`: `/var/lib/rfspager` could not be created; config
  still applies but nothing is cached and updates are refused.
- `live gain change failed; restarting dongle`: librtlsdr rejected the value
  (not every tenth of a dB exists); the dongle was reopened with the config.

## Weak-signal decoding (5.6)

What the decoder does for distant pages, and how to measure a change
before deploying it:

- Per-baud IF filtering. Each baud leg filters the 12 kHz channel down to
  what that baud needs (6.5 kHz for 512, 7.5 kHz for 1200) before its own
  FM discriminator. This moves the FM threshold by about 3 dB, which is
  where far pages live.
- Alignment is preserved through unrepairable codewords: their raw bits
  are kept (at most four wrong characters) instead of dropping 20 bits and
  garbling the rest of the page. The damaged spans, counts of corrected and
  unrepairable codewords and a truncated flag travel with the page as
  `quality` and appear in the receiver's `page` log lines.
- BCH repairs are checked against the codeword's even parity; a repair that
  fails parity is a mis-correction and is treated as unrepairable.
- Sync is hunted with two bit errors, tracked with three once locked, and
  looked for from two bits early to three bits late, so a slipped bit
  realigns the batch instead of ending the page.
- Soft decision: the slicer reports a confidence per bit; when BCH gives up
  the five least confident bits are retried in every combination (Chase-2)
  and accepted only if every changed bit was doubtful. About another
  decibel.

A full-bit integrate-and-dump decision was tried and measured worse than
the half-bit matched filter sampled mid-symbol at every noise level, so the
original decision stays.

Measure before you deploy:

```sh
RFS_SNR_SWEEP=1 RFS_SNR_TRIALS=12 go test ./internal/receiver -run TestSNRSweep -v
```

prints, per in-band SNR, how many synthetic RFS pages decoded exactly, with
minor damage, not at all, or as garbage, plus phantom pages on other
capcodes. Baseline before this work: exact decodes collapsed between 6 and
4.5 dB. After: exact to 1.4 dB, no phantoms above 0 dB. Real captures are
the other half: pull `iq` captures of weak pages from the Diagnostics tab and
run them through `cmd/replay`, which uses the same leg pipeline.
