# Capcode 0521839 — the once-a-minute data beacon on FRNSW

_Notes written 2026-09-21 while fixing the receiver decoder. Everything below is
observed from our own receivers plus the prod database; the interpretation of the
payload is inference, not documentation from the network operator._

## What it is

| | |
|---|---|
| Capcode | `0521839` (POCSAG address 521839, frame 7) |
| Channel | FRNSW, 148.9625 MHz |
| Baud / function | 512 baud, function A (`fn=0`) — 99.8 % of decodes; the handful of other `fn` values in the pre-fix log are decode errors |
| Cadence | **one page every 60 s**, logged at hh:mm:14–15 AEST, around the clock |
| Content | always exactly **60 characters of base64** (59 + one `=` pad) → **44 bytes** |
| Seen at | Budgewoi (Central Coast) and Figtree (Illawarra) — identical bytes at both sites, so it is a network-wide broadcast, not a local transmitter quirk |
| PagerMon | also has it set to `ignore=true` with the same alias |

It was named "Rubish" because, until today, we only ever decoded fragments of it:
the old receiver was shedding ~18 % of IQ samples, so a 0.8 s burst almost never
survived intact. What reached the database looked like `UpDtF+q23kX`, `XmbuJ&m1`,
`ur7tFygrDUsIE4pgY* 3` — random-looking, wrong length, unprintable characters. With
the decoder fixed it is a perfectly regular, fully printable, fixed-length payload.

### Decode loss, before vs after the fix (Budgewoi journal)

| Window | Transmitted (1/min) | Logged at all | Logged intact (60-char base64) |
|---|---|---|---|
| 2026-09-14 16:53 → 2026-09-21 22:16 (173 h, old decoder) | ≈ 10402 | 2264 | **2** |
| 2026-09-21 22:49 → 2026-09-21 23:31 (43 min, new decoder) | 43 | 43 | **43** |

Every minute since the new binary went live has been decoded intact. This capcode
is, incidentally, the best free health check we have: it is on air 1440 times a day
with a known structure. If a receiver misses minutes or the decoded length is not 60,
that receiver has a problem.

## Payload structure

Base64-decode the 60 characters and you get 44 bytes:

```
offset  size  content
0       4     uint32 little-endian, increments by exactly 60 each message
4       40    high-entropy bytes (7.9 bits/byte across 45 samples, all 256 values seen)
```

The 4-byte counter is a **seconds clock**. Fitting it against our log timestamps gives
an epoch of **2014-01-01 00:00 UTC** (to within ~16 s, which is about the burst
length plus the transmitter's queueing delay). So the beacon carries the network's
notion of "now" as seconds since 2014-01-01, and the remaining 40 bytes are either
encrypted or an authentication tag (e.g. 8 bytes of something + a 32-byte HMAC, or
an AES block or two). Nothing in the tail repeats or correlates with the counter, so
it is not a simple checksum over the clock.

Plausible readings, in rough order of likelihood:

1. A **paging-network keep-alive / time-sync beacon** from the FRNSW paging
   controller, with an authenticated timestamp so receivers can detect replay.
2. An **over-the-air control channel** for data pagers or telemetry units that share
   the FRNSW frequency, using capcode 521839 as a group address.
3. A **test loop / link monitor** injected by the operator (less likely: those are
   normally human-readable).

Decode helper:

```python
import base64, datetime
EPOCH = datetime.datetime(2014, 1, 1, tzinfo=datetime.timezone.utc)
raw = (401460546).to_bytes(4, "little") + bytes(40)  # synthetic example
ctr = int.from_bytes(raw[:4], "little")
print(len(raw), ctr, EPOCH + datetime.timedelta(seconds=ctr), raw[4:].hex())
# 44 401460546 2026-09-21 12:49:06+00:00 00000000...
```

## Where it is filtered

- **Receiver** — `internal/receiver/pipeline.go` `shouldPost`: any alpha message longer
  than 12 characters with no space is treated as noise and logged with `posted=false`.
  This beacon is therefore never posted. That is the right default for a dispatch
  feed, but it means the clean payloads exist only in the Pi journals. If you want to
  keep them (as a receiver health signal, or to try to make sense of the tail),
  the cheapest option is a per-capcode allow-list in the receiver, or a separate
  "data" post path that bypasses the printable-text heuristic.
- **Prod DB** — `capcodes.ignore = true`. Ignored capcodes are rejected at Go ingestion. Historical rows should be
  reviewed as part of a separate data-quality migration.
- **PagerMon** — `ignore=true`, so it never reached Laravel via that path either.

## Local diagnostics

Run `journalctl -u rfspager-receiver` on a receiver you operate and filter by
`address=0521839`. Compare counts and decode completeness against the one-minute
cadence. Receiver addresses, database record IDs, and captured payload archives
are operational material and are not part of the public repository.

Use synthetic payloads in tests. The examples above describe observed structure;
they are not a specification from the network operator.
