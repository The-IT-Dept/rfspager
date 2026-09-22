# Adapty backfill

One-off command to pull every user's subscription state out of Adapty's
Server-Side API and write it onto our `users` row. Use this **once**, as
part of moving off Adapty, so the new direct-to-store IAP pipeline has
the data it needs:

- Show correct subscription status for paid users who haven't opened the
  app since the cutover.
- Populate `apple_original_transaction_id` and `google_purchase_token`
  so future Apple S2S notifications and Google RTDN events can find the
  matching user without waiting for the app to call `/iap/restore`.

After this runs once, you can revoke the Adapty API key. The command
isn't designed for repeated use — Adapty is no longer the source of
truth.

---

## 1. Get the Adapty secret key

**Adapty Dashboard → App Settings → General → API keys → Secret API key.**

Copy the `secret_live_...` value. (The `public_live_*` key won't work
for the Server-Side API.)

```bash
export ADAPTY_SECRET_API_KEY=secret_live_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

## 2. Dry-run on a handful of users

Always start with a dry-run so you can eyeball what would change:

```bash
go run ./cmd/adapty-backfill \
  --dry-run \
  --only-missing \
  --limit 10 \
  --verbose
```

Sample output:

```
backfilling 10 user(s) from Adapty (dry-run=true, only-missing=true)
  41 7E3F…6A9B — apple io.devlan.rfspager.yearly expires=2027-01-05 renew=true store_id=2000000567891234
  42 9C1D…44A8 — not in Adapty
  43 BB7A…0E11 — no paid access in Adapty
  44 C821…77FE — google io.devlan.rfspager.monthly expires=2026-06-12 renew=true store_id=hkdpolajbgnmm…

result: updated=0 cleared=0 not_in_adapty=1 no_access=1 errored=0
```

Verify a couple of the lines against what Adapty's dashboard shows for
those users. Pay attention to:

- **`store_id=…`** — for Apple, this is the `original_transaction_id`
  (15-19 digit integer); for Google, it's the long alphanumeric
  purchase token. If you see neither populated for users who you *know*
  are paid, see [Troubleshooting](#troubleshooting).
- **`renew=true/false`** — confirms whether the user has cancelled.
- **`expires=…`** — should be in the future for active subscribers.

## 3. Run it for real

Drop `--dry-run` and `--limit`:

```bash
go run ./cmd/adapty-backfill --only-missing --verbose | tee adapty-backfill-$(date +%F).log
```

Saved log is your audit trail. Each updated user also gets a row in
`iap_transactions` with `event_type='backfill:adapty'` so you can
reconstruct what we wrote even if you lose the stdout log:

```sql
SELECT user_id, platform, product_id, expires_at, will_renew,
       LEFT(raw_payload, 200) AS payload_preview
FROM iap_transactions
WHERE event_type = 'backfill:adapty'
ORDER BY created_at DESC
LIMIT 20;
```

The command sleeps `200ms` between Adapty calls by default (5 req/s,
well under Adapty's rate limit). For ~10k users that's ~30 minutes;
override with `--sleep 100` if you want to push harder.

## 4. Spot-check a real renewal flow

Pick one of the updated Apple users, then in the App Store Server
Notifications screen ("Request a Test Notification" — see
[`iap-webhooks.md`](./iap-webhooks.md)) trigger a notification for that
account. Watch the server logs:

```
apple webhook received  type=DID_RENEW  subtype=
apple webhook: user updated  original_txn=2000000567891234  valid=true  rows=1
```

`rows=1` is the proof the backfilled `apple_original_transaction_id`
matched. If you see `rows=0`, the ID we wrote doesn't match what Apple's
notification carries — see [Troubleshooting](#troubleshooting).

---

## Flags

| Flag             | Default              | What it does                                                                 |
| ---------------- | -------------------- | ---------------------------------------------------------------------------- |
| `--dsn`          | `$DATABASE_URL`      | Postgres connection.                                                         |
| `--adapty-key`   | `$ADAPTY_SECRET_API_KEY` | Adapty secret API key.                                                   |
| `--only-missing` | `true`               | Skip users who already have an Apple or Google store ID on file.             |
| `--user-id`      | (none)               | Backfill a single user — accepts either `users.id` integer or `user_id` UUID.|
| `--limit`        | `0`                  | Stop after N users (0 = no limit). Pair with `--dry-run` for safe testing.   |
| `--sleep`        | `200`                | Milliseconds between Adapty API calls. Lower = faster, higher = friendlier.  |
| `--dry-run`      | `false`              | Print what would change, don't write to the DB.                              |
| `--verbose`      | `false`              | Log every user attempted, not just changes.                                  |

---

## What gets written

For each user where Adapty knows about a paid access level:

| Column                                | Source on Adapty side                            |
| ------------------------------------- | ------------------------------------------------ |
| `subscription_valid`                  | `expires_at` in the future AND not `refunded_at` |
| `subscription_expiry`                 | `expires_at`                                     |
| `subscription_product_id`             | `store_product_id`                               |
| `subscription_platform`               | `store` → `apple` / `google` / `stripe`          |
| `will_renew`                          | `renew_status == 1` (or `renewal_cancelled_at` is null) |
| `cancellation_reason`                 | `cancellation_reason` (only when non-empty)      |
| `apple_original_transaction_id`       | `store_original_transaction_id` when platform=apple  |
| `google_purchase_token`               | `store_original_transaction_id` when platform=google |
| `last_store_sync_at`                  | `NOW()`                                          |

Plus one append-only audit row in `iap_transactions`:

```
platform, transaction_id, original_transaction_id, product_id,
event_type='backfill:adapty', is_valid, expires_at, will_renew,
environment, raw_payload=<the entire Adapty JSON response>
```

---

## What does **not** get written

- **No downgrades.** If Adapty doesn't know about a user, or reports no
  paid access for them, we leave the row alone. The reasoning: an
  optimistic Adapty record is safer than a pessimistic one, because
  the in-app `/iap/restore` will reconcile genuine non-subscribers the
  next time they open the app. If we wrote `subscription_valid=false`
  here we'd risk briefly downgrading someone whose Adapty record was
  stale.
- **No webhooks fired.** Backfilled rows don't trigger any push.

---

## Troubleshooting

**`store_id` is blank for users you know are paid.**
Adapty's v2 API populates `store_original_transaction_id` for purchases
made through their SDK from late 2022 onwards. Very early-era purchases
may have only `vendor_transaction_id` (which doesn't survive renewals
and is useless for our webhook matching). Those users will get correct
`subscription_valid` + `expiry` from the backfill, but won't have a
store ID — they'll be reconciled the next time they open the app and
hit `/iap/restore`, which validates with Apple/Google directly and
returns the real `original_transaction_id`.

**`rows=0` from the Apple webhook after backfill.**
The `apple_original_transaction_id` we backfilled doesn't match what
Apple's signed notification carries. Two usual causes:

1. The Adapty record is stale — the user renewed via a different Apple
   ID, or restored on a new device, and Adapty never saw the new ID.
2. The user's purchase was sandbox-only.

Compare the JWS payload's `originalTransactionId` field (visible in
admin notification history) against `users.apple_original_transaction_id`.
If they differ, reconcile the affected accounts against the App Store record
before changing either binding. Migration 023 makes an entitlement unique
across users; do not use a blind `UPDATE`, because it can either fail on the
unique constraint or hide a cross-account ownership conflict.

**`rate-limited (429)`.**
Increase `--sleep` (e.g. `--sleep 500`) and resume. The command is
idempotent — running it again with `--only-missing` will skip users
already backfilled.

**`not in Adapty`.**
Either the user signed up after Adapty was removed from the app, or
their `users.user_id` doesn't match Adapty's `customer_user_id`. The
mapping is what Laravel used:
`adapty-customer-user-id: <users.user_id>` (UUID). If you suspect a
mapping bug, run with `--user-id <id>` on a single known account and
compare against the Adapty dashboard.

---

## After backfill — clean-up

When you're confident the data is good:

```bash
# Revoke the Adapty secret in their dashboard.
unset ADAPTY_SECRET_API_KEY
```

Nothing else needs removing — there's no Adapty code left in the Go
server (only this one-shot command).
