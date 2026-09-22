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
| Prod DB | `capcodes.id = 252215`, alias/agency **"Rubish"**, `ignore = true`, created 2024-05-14. 4031 messages stored against it (system 2, all pre-fix and therefore all mangled) |
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
raw = base64.b64decode("Qs3tF1LF+wBQOPMqrITw57ebu3kQ1r20ANntruV6Fj1NIYqbZclgBHZnH9E=")
ctr = int.from_bytes(raw[:4], "little")
print(len(raw), ctr, EPOCH + datetime.timedelta(seconds=ctr), raw[4:].hex())
# 44 401460546 2026-09-21 12:49:06+00:00 52c5fb00...
```

## Where it is filtered

- **Receiver** — `internal/receiver/pipeline.go` `shouldPost`: any alpha message longer
  than 12 characters with no space is treated as noise and logged with `posted=false`.
  This beacon is therefore never posted. That is the right default for a dispatch
  feed, but it means the clean payloads exist only in the Pi journals. If you want to
  keep them (as a receiver health signal, or to try to make sense of the tail),
  the cheapest option is a per-capcode allow-list in the receiver, or a separate
  "data" post path that bypasses the printable-text heuristic.
- **Prod DB** — `capcodes.ignore = true`. Nothing on the Go read side consults this
  flag yet (see the prod-infra notes), so the 4031 mangled historical rows are still
  visible to any query that does not filter on it.
- **PagerMon** — `ignore=true`, so it never reached Laravel via that path either.

## How to find it

On either Pi (Budgewoi `rfspager@192.168.15.52`, Figtree `root@100.79.78.79`):

```sh
# every decode of this capcode, newest last
sudo journalctl -u rfspager-receiver -o short-iso --no-pager | grep 'address=0521839'

# live
sudo journalctl -u rfspager-receiver -f | grep --line-buffered 'address=0521839'

# minutes missed in the last hour (should print nothing)
sudo journalctl -u rfspager-receiver --since '1 hour ago' -o short-iso --no-pager \
  | grep 'address=0521839' | cut -c12-16 | sort -u | wc -l   # expect 60
```

In prod Postgres (apps server, `sudo -u postgres psql -d rfspager`):

```sql
SELECT * FROM capcodes WHERE address = '0521839';

SELECT m.created_at, m.system_id, m.message
  FROM messages m JOIN capcodes c ON c.id = m.capcode_id
 WHERE c.address = '0521839'
 ORDER BY m.created_at DESC;
```

Raw journal extracts as of 2026-09-21 23:31 AEST are checked in next to this file:

- `docs/capcode-0521839-budgewoi.log` — 2307 lines, 2026-09-14 → 2026-09-21, includes every garbled pre-fix decode
- `docs/capcode-0521839-figtree.log` — 26 lines, from first start 2026-09-21 23:07

## Every intact decode so far

One row per minute. **Site**: B = Budgewoi, F = Figtree, B+F = both receivers heard it
(payload was byte-identical in every such case). **Counter** is bytes 0–3 as uint32 LE;
**Clock** is that counter rendered as AEST time-of-day from the 2014-01-01 UTC epoch.

| Logged (AEST) | Site | Counter | Clock | Payload |
|---|---|---|---|---|
| 2026-09-19 17:52:22 | B | 401269926 | 17:52:06 | `puTqF5R+XkexaQZR0zVPp3v5gkipIJR5IsC0hstf6AsHWUmVSkZMVEPq3LY=` |
| 2026-09-21 15:04:15 | B | 401432646 | 15:04:06 | `RmDtF1sV3hlYM6OEE9FBoWCxWgriLo+IdOe3r0OAmvAJrv8Nc2+41pXujtc=` |
| 2026-09-21 22:49:14 | B | 401460546 | 22:49:06 | `Qs3tF1LF+wBQOPMqrITw57ebu3kQ1r20ANntruV6Fj1NIYqbZclgBHZnH9E=` |
| 2026-09-21 22:50:14 | B | 401460606 | 22:50:06 | `fs3tF5y+uRS9TdeGPZ2XZ6P9dO9X92lF/APMBkUElMwY4LuBrn7fk5kes3Y=` |
| 2026-09-21 22:51:14 | B | 401460666 | 22:51:06 | `us3tF5LV1DP02VPGfBwVMLs25sbnhABVUByghAOvJaSabREcIASwHyMyzyQ=` |
| 2026-09-21 22:52:14 | B | 401460726 | 22:52:06 | `9s3tFxquSxP50i7oPKCDNKUZuHqC4Uzi8Fx3oM/lfercNRUMCQwNDrs0pJk=` |
| 2026-09-21 22:53:14 | B | 401460786 | 22:53:06 | `Ms7tF7bXtyco8/zQjUCMDoSnsdqSN6EtQmaN3K8O+e1QHC8GBfg9qc1Sij0=` |
| 2026-09-21 22:54:14 | B | 401460846 | 22:54:06 | `bs7tF8n3uQ2y3qFYq7S+p51iTpsdxByQEE5NIRk/77TxUrBqGI85iOn6tik=` |
| 2026-09-21 22:55:14 | B | 401460906 | 22:55:06 | `qs7tFwqO5G8TCkjpqDYObNzkgzCXvspzBilcQWD1oThMGAZa1aLBWrgtpio=` |
| 2026-09-21 22:56:14 | B | 401460966 | 22:56:06 | `5s7tF0VN8WHUqqofkhm81v9gBocyRvdDJgg0F5+3Nh/5BpEB7Dgo0ZrXmFM=` |
| 2026-09-21 22:57:15 | B | 401461026 | 22:57:06 | `Is/tF9D8p1AraadCiuC6yEU94k80kRTXn5oEygcT6XY4EUPwyvYb8De/y0c=` |
| 2026-09-21 22:58:15 | B | 401461086 | 22:58:06 | `Xs/tF/tonja+BVHObLohHrKimOqb5nu1Z5MPqiZMaM9OCR3+rn2d/0mDH1Y=` |
| 2026-09-21 22:59:15 | B | 401461146 | 22:59:06 | `ms/tFzhKhB4X8kMNA4fYsHCIFEBjW1+cU8VxVBb+cUjhn5ZIAJGZsM/9ntU=` |
| 2026-09-21 23:00:15 | B | 401461207 | 23:00:07 | `18/tFzOcjyx1plud7vGJNnAy4nvyQKBptHx1hJSPaPNngTSeozz4PUWmUN0=` |
| 2026-09-21 23:01:14 | B | 401461266 | 23:01:06 | `EtDtFxnKUjr1LA5zNDbzqEKbS7hnL36bXzbWS4Bx6oUMnMMWlbieOpeQyHA=` |
| 2026-09-21 23:02:14 | B | 401461326 | 23:02:06 | `TtDtF1VSOTgRwB7LDOjN5AqgELAxb5UOi/5om0b22yN+vH0Wu+Qv3qjXnSI=` |
| 2026-09-21 23:03:14 | B | 401461386 | 23:03:06 | `itDtF/JqPRTgpEPqWPnKzyTX2vJQiZ+HGyHCNvip1QwrNADIgSu8FPcMhwU=` |
| 2026-09-21 23:04:14 | B | 401461446 | 23:04:06 | `xtDtF7i+x1liAW1HMHD5JdrHeMrprpQW0CgQdnky7UmbHD3S7j0AihenTk0=` |
| 2026-09-21 23:05:14 | B | 401461506 | 23:05:06 | `AtHtF8Bqcn2we+myOI/phCylYkB7LoChq2vrhYej29VYsvhSa42v+XJhWMo=` |
| 2026-09-21 23:06:14 | B | 401461566 | 23:06:06 | `PtHtFzcSkWHTcPa9+LCnnou+M54LlcWNn9WECcv8O6iTUaj5aU2qHrETtlw=` |
| 2026-09-21 23:07:14 | B | 401461626 | 23:07:06 | `etHtFzPQcSYYGjhYkNi3kh6UY5yeRksGkTFgkq/VVBJlYbx2IDYmuBC9zYo=` |
| 2026-09-21 23:08:14 | B+F | 401461686 | 23:08:06 | `ttHtF8T4xlhYrunQHU6fshuobjD6YpTIcFoI9XfT7288NNUq9kZbi9IN4v4=` |
| 2026-09-21 23:09:14 | B+F | 401461746 | 23:09:06 | `8tHtFz+ydHuSpvFPgcjmRHEBID6p4fQYhHzB9n3d+ayh2fowq6VVdEU9rY8=` |
| 2026-09-21 23:10:14 | B+F | 401461806 | 23:10:06 | `LtLtF3jEoVct/Q9umZXO0sZxlqnXTJk2/thhWOOttLf9kdlXHU5lDiPjjJY=` |
| 2026-09-21 23:11:14 | B+F | 401461866 | 23:11:06 | `atLtFyR49h24oyv2QERX4ywlqyhjqBD6YHtbzlCQyV+xUnmyS6SpXhKzW/o=` |
| 2026-09-21 23:12:14 | B+F | 401461926 | 23:12:06 | `ptLtF7WM7xyLfKBtBnhnzlSCnRZVWVt6KKe/xA4arrH23ttS0h/mATGEWCw=` |
| 2026-09-21 23:13:14 | B+F | 401461986 | 23:13:06 | `4tLtFx8tNEGw3YjmW+gJQXq4ocXAyGdz3k/tlGJyK3f5Wv0qonFtxC6Dc7Q=` |
| 2026-09-21 23:14:14 | B+F | 401462046 | 23:14:06 | `HtPtF6aOpmeZqBJpgezHcRhTZbdhBxaNfNsCzN2d1vly8EG70VIClfmE4uA=` |
| 2026-09-21 23:15:14 | B+F | 401462106 | 23:15:06 | `WtPtFzTiDyCyz+72t2RTri97r3yLo8wnfD0wkq7m6/Xqmo5sbu67ZgZsGnc=` |
| 2026-09-21 23:16:14 | B+F | 401462166 | 23:16:06 | `ltPtF9iIJGKAJ76DnWbZI17iWIe2EUcaRRrpe1bt3mQaIa/iNpHJDT9NorI=` |
| 2026-09-21 23:17:14 | B+F | 401462226 | 23:17:06 | `0tPtF6s9dEuOl7cnWACufFeklCGyp+rNpRkHofmFsEeOdsKNnMf/cVT+4KM=` |
| 2026-09-21 23:18:14 | B+F | 401462286 | 23:18:06 | `DtTtF7WtAAqmenmkzmrtyJXhmzxQCQM6oLHqCoK57Dv2/aQdJ2s3yg/0K2s=` |
| 2026-09-21 23:19:14 | B+F | 401462346 | 23:19:06 | `StTtFwrukC/qa3A+Lw/Ib2kYOwHoSOlvLGsJ/Uo5HNsNeVTeanmnEUaNzd8=` |
| 2026-09-21 23:20:14 | B+F | 401462406 | 23:20:06 | `htTtF8Vp9HVHBorIw8tYB+UyWeyWqk8rDSu8AV96g1GmsvaOjp58yNH9d0Y=` |
| 2026-09-21 23:21:14 | B+F | 401462466 | 23:21:06 | `wtTtF+aUXB32nCazz6IpSvFgcm+qjdL26mTaqMGZDPPNq11VfBQTMTCTsE8=` |
| 2026-09-21 23:22:14 | B+F | 401462526 | 23:22:06 | `/tTtF04ujnXybNEOjh1jKYLfH3NPh5dXwIZFdJ/343SpIqK8x5RkrOKBWDw=` |
| 2026-09-21 23:23:14 | B+F | 401462586 | 23:23:06 | `OtXtFxatZlXvpP6wgpwhYHumS+p7BYKgiCo6Mu8M5bri5BF8oi+sbUdVjzk=` |
| 2026-09-21 23:24:15 | B+F | 401462646 | 23:24:06 | `dtXtF9rUwUibnp5lf9wlLqDusJsSB5wIs4ImexLjTy2ucSRd3MgkHV453PE=` |
| 2026-09-21 23:25:15 | B+F | 401462706 | 23:25:06 | `stXtF6n8xGzXhg17FtOSaTi8Iqqovv9fS0zSlwz8OjKYbAX5l9ECH1yOWRE=` |
| 2026-09-21 23:26:15 | B+F | 401462766 | 23:26:06 | `7tXtF0pcdBnTk2uF1IJRBgCfJ9NNnN9p0pZqaQld0+x+2GqgdWFFN6Kudwo=` |
| 2026-09-21 23:27:14 | B+F | 401462826 | 23:27:06 | `KtbtFyFflWmsyQy7Uau5qrju2ObVHrLgirT0z0XJ6JFfKZdU5CUV3FUNXiI=` |
| 2026-09-21 23:28:14 | B+F | 401462886 | 23:28:06 | `ZtbtF/B5UjoiMX8xQyTWHNi7tPlSZnPa2ksre50i4HcHNUs7grsQMkh6LiA=` |
| 2026-09-21 23:29:14 | B+F | 401462946 | 23:29:06 | `otbtF4BGImMdmQx8zecySkV9050eEdV3fa0NLmrk/Ycn0Gz/jpawr1zkv84=` |
| 2026-09-21 23:30:14 | B+F | 401463006 | 23:30:06 | `3tbtF1FQohZX9rctdrQ5wSjhhOmDcL5YgEeOsZibI8hGASmgfKSOXlNYSIU=` |
| 2026-09-21 23:31:14 | B+F | 401463066 | 23:31:06 | `GtftF0JjQxeBkIPXFMY9G5QkE+GtgYqUj9xZAeDWU7M4zIUmjPM54m+ZFi4=` |
| 2026-09-21 23:32:14 | F | 401463126 | 23:32:06 | `VtftF7S7GFlAElvxW9gYaz5nCIWTv5C5KLYnBAA5WWmHuZQX8JjqG1zRuI0=` |
| 2026-09-21 23:33:14 | F | 401463186 | 23:33:06 | `ktftF1Ru6nj3fYmtO3qTKY5LnJQC6CactKdGXbIaA1cJzkamFkW7SSsMiRY=` |

Total: 47 distinct minutes, 71 decodes.
