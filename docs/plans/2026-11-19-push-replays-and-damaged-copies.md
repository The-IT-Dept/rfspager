# Replayed pages and damaged pushes (2026-11-19)

## Problem

Two user-visible push bugs, diagnosed against prod on 2026-11-19 from a user report.

1. **Replays re-pushed.** The RFS paging network now and then re-transmits hours to
   days of backlog in a few minutes (bursts on 8, 9, 13 Oct, 29 Oct, 4 Nov, 8 Nov and
   19 Nov 2026; ~6,500 pushes). Receivers hear them as live pages. The dedupe matched
   an RFS header only within ±5 min and nothing compared a page's own header time with
   when it was heard, so every replayed page was stored and pushed again.
2. **Damaged pushes.** Up to six receivers hear one simulcast page within a second.
   Fan-out pushed whichever copy was stored first (pushes leave 2–4 s later). A better
   copy then replaced `messages.message` (what app and web show), but the push had gone
   with the damaged text: the upsert's "rewrite unsent pushes" step raced fan-out's
   uncommitted INSERT or ran after the dispatcher claimed the rows. Separately the
   one-way `textSimilarity` scored a clean full copy 0.43 against a truncated damaged
   copy stored a second earlier, so the page was stored and pushed twice. 5–12 % of
   pushed pages per day carried text later repaired.

## Decisions

| # | Rule | Value |
|---|------|-------|
| 1 | RFS header dedupe lookback | 72 h |
| 2 | Stale page: header older than reception by more than | 30 min → store, don't push (when corroborated, see below) |
| 3 | A clean copy following a damaged fragment of the same transmission merges into it | — |
| 4 | Hold a damaged copy before fan-out, push the least damaged copy | up to 30 s |
| 5 | Correction push when a better copy fixes an already-pushed page | yes |

A design review (four adversarial reviewers: concurrency, push delivery, dedupe,
failure modes) and a code review (five lenses, each finding verified by a skeptic;
9 of 10 confirmed and fixed) reshaped several of these; the reasons are recorded below.

## Page facts — `internal/pagetext`

- `DispatchTime(text, near)`: a leading `D Month YYYY HH:MM:SS` (RFS/SES) or
  `YYYY-MM-DD HH:MM:SS` header as Australia/Sydney time. Both +10:00 and +11:00 are
  tried; a reading is valid when Sydney's offset at that instant equals it; the one
  closest to `near` wins (the repeated hour at the end of DST). Embeds `time/tzdata`.
- `Damaged(text, q)`: unrepairable, truncated or half-corrected (`Suspect`); without
  a quality record, any garble character.
- `HeaderTrusted`: not heavily corrected, and no unrepairable range overlaps the
  header (if anything is unrepairable the ranges must be known). Without quality:
  no garble character anywhere.
- `StaleHeader(text, q, heardAt)`: trusted header, lag > 30 min and < 7 days.
- `ReplayWatch`: every receiver's stale copy is observed at ingest (before the dedupe
  drops any) and again at fan-out. A **burst** is ≥ 3 distinct stale pages within
  10 min whose lags span ≥ 30 min. A single stale page (a header digit repaired
  wrongly) or a constant lag (a paging or receiver clock fault, including half-hour
  zone mix-ups) is not a burst and is pushed: a double push beats silence.
- `IncidentID`: the RFS `26-NNNNNN` or FRNSW `INC: N-DDMMYYYY` number, if no character
  of it came from an unrepairable codeword.

Validation on 45 days of prod header pages (10,354): no live page is stale with a
trusted header outside the seven bursts; inside them the November bursts are
caught 33/34, 37/39, 26/26 at the page level (the 72 h header match catches most
replay copies before fan-out anyway: 33 of 35 on 19 Nov).

## Dedupe — `internal/db/messages_dedupe.go`

Rules, most specific first: identical text ±5 min; RFS header ±5 min; first 30
characters; the heavy-repair similarity rule within the 3-min resend window; then
the **72 h header lookback**; then the phantom-capcode drop.

- The lookback runs after the recent rules so a clean copy of a replay repairs a
  damaged copy of the same replay stored a second earlier rather than matching the
  day-old original (which would leave the damaged copy to be pushed).
- Header regex accepts one or more spaces after the time (SES) and the ISO form.
- `textSimilarity` is unchanged (one-way). The new `storedFragmentOf` handles a stored
  damaged fragment followed by a complete copy: fragment ≥ 16 chars, prefix-aligned
  similarity ≥ 0.85, and only within 15 s (the simulcast case), because a fragment's
  opening reads like any page of the same format to the same capcode.
- `differentPages` vetoes the similarity rule when both copies carry trusted
  headers more than 2 s apart, or clean incident numbers that differ (a follow-up page
  to the same brigade is not a re-send). Neither is trusted from a heavily corrected
  copy: a repair can land on the wrong digit.
- The fragment rule works in either arrival order (`fragmentOfSameTransmission`).
- A copy with the same text but a better decode upgrades the stored quality (text and
  `replaced_count` untouched), so fan-out releases a held copy whose text turned out
  right; if the stored copy was damaged and already fanned out, fan-out reopens.

### Replacement → notifications (`carryToNotifications`)

Runs in the upsert transaction after the `UPDATE messages` that locks the row, and
first locks the message's live notification rows (`FOR UPDATE`) so the dispatcher
can't move a row between the statements (claims skip them; `markSent` waits and then
decides from the committed row, which is why its logic lives in its SET expressions
rather than a CTE):

1. unsent (`pushed_at NULL, attempts 0`, not a correction): carry the new text;
2. pending correction: update `correction_body`;
3. in flight / retrying: carry the new text with `carried_fix` (= undamaged and within
   10 min). A retry sends the best text there is, as an ordinary audible push. If the
   send in flight succeeds, `markSent` makes a correction only when `carried_fix`;
   otherwise it records the text that actually went out;
4. delivered within 10 min, not yet corrected, if undamaged: queue a correction —
   `correction: true`, `correction_body`, `first_pushed_at`, `pushed_at = NULL`.
   `message`/`body` keep what was actually pushed (the admin page compares them).

`rfs_dispatch` is notified whenever any row was touched. If the new copy is undamaged
and the message was fanned out within 10 min, `fanned_out_at` is cleared and
`rfs_refanout` notified: fan-out decides again on the clean copy (alertzones from
corrected coordinates, a damaged re-send that was suppressed, a replay decision); the
per-user `NOT EXISTS` keeps anyone already notified from a second push.

## Fan-out — `internal/dispatch/fanout.go`

Pre-check per message (PK lookup), with an in-memory `held` map owned by `Run`:

- damaged (and heard < 2 min ago), or stale and not yet a corroborated burst → hold;
  re-checked every second; released as soon as an undamaged copy replaces it, or at
  30 s from `min(first seen, heard)`;
- at release: stale and a burst → `fanned_out_at`, `push_skip_reason = 'replay'`
  (unless the message already has notifications: a reopened, already-pushed page),
  `push_lag_s`, no notifications; otherwise fan out and record `push_hold_ms` /
  `push_lag_s` (earlier values kept when a reopened message is re-decided).
- `fanoutSQL` locks the message row (`FOR NO KEY UPDATE OF m`) under a 5 s
  `lock_timeout`: a replacement in progress commits first and its text is read; a
  later one waits and then sees (and corrects) the committed notification rows.
  Duplicate copies never touch the row and never wait (prod fan-out takes 60–310 ms).
- `rfs_refanout` (new channel, fan-out only; SSE brokers ignore it) re-runs a message.

## Dispatch and push — `internal/dispatch/dispatcher.go`, `internal/notify`

- `markSent` stamps `pushed_at` only if `data` is still what was claimed. Otherwise the
  row becomes a correction (claimed data restored + `correction_body` + `first_pushed_at`)
  and is requeued — unless the claimed row was already a correction (one correction per
  push), or some device failed retryably (requeued as an ordinary, loud push).
- `markFailed` on a correction restores the original delivery instead of marking failed,
  and flags `correction_failed` (the admin page shows "correction not delivered").
- Every page push carries collapse key `msg-<message id>`: APNs `apns-collapse-id`,
  FCM Android `notification.tag` plus `data.collapse`. A correction: title +
  " (corrected)", text from `correction_body`, no sound, `apns-expiration: 0` / FCM
  TTL 0 (deliver now or never — APNs keeps one notification per app for an offline
  device, so a stored correction could displace a newer page), Android channel
  `corrections`.
- No FCM `collapse_key`: FCM keeps only four per offline device and would drop pages.

## App — `apps/mobile`

Foreground display uses `data.collapse` as the notifee id (corrections replace in
place) and the `corrections` channel for corrections; the app creates that channel
(low importance, no sound). Until that release, FCM falls back to the default channel
for corrections (they still replace the original in the tray, with sound).

Known limitation: on Android the FCM SDK (app in background) posts a page as
`(tag "msg-N", id 0)` and notifee (app in foreground) as `(no tag, hash("msg-N"))`.
A correction replaces the original only when both took the same path; if the app
changed state in the minute between them, the correction appears beside it.
Matching FCM's key would need a notifee id whose Java hash is 0, which is not worth
the hack.

## Admin

Migration 041 adds `messages.push_skip_reason`, `push_lag_s`, `push_hold_ms`; the admin
message page shows a "Push decision" line and, per notification, the correction text
and both push times.

## Verifying in prod

```sql
-- held, replayed, late-but-pushed pages since deploy
SELECT id, address, created_at, push_skip_reason, push_lag_s, push_hold_ms, left(message, 60)
  FROM messages WHERE created_at > :deploy AND (push_skip_reason IS NOT NULL OR push_hold_ms IS NOT NULL OR push_lag_s IS NOT NULL)
 ORDER BY created_at DESC;
-- corrections (reach notifications through messages; never scan notifications)
SELECT m.id, (SELECT count(*) FROM notifications n WHERE n.message_id = m.id AND n.data::jsonb ? 'correction_body') AS corrections
  FROM messages m WHERE m.created_at > :deploy AND m.replaced_count > 0;
```
Logs: `fanout: holding`, `fanout: releasing held message`, `fanout: replayed page not
pushed`, `correction pushes queued`, `dispatch: page corrected while in flight`.
