# Subscription notifications — App Store + Play Store setup

This is the operator's runbook for wiring **server-to-server subscription
notifications** so the Go server learns about subscription lifecycle events
(renewals, cancellations, refunds, billing failures, grace periods) the
moment Apple or Google know about them — instead of waiting for the next
time the user opens the app and we re-validate.

The server already has the handlers. This document covers what to do in
**App Store Connect** and the **Google Play / Google Cloud consoles** to
point each store at us, and what those events do to a user's
`subscription_valid` / `will_renew` flags.

> **Production URLs**
> - Apple: `POST https://<your-host>/api/webhooks/apple`
> - Google: `POST https://<your-host>/api/webhooks/google`
> - Stripe (web fallback): `POST https://<your-host>/api/webhooks/stripe`
>
> All three are public, but each handler verifies the signature on the
> incoming request before trusting any of the body. Don't put them behind
> additional auth — that breaks the store side.

---

## How the server reacts

Both handlers update the matching row in `users` and append an audit
row to `iap_transactions`. The columns that change:

| Column                      | When                                                              |
| --------------------------- | ----------------------------------------------------------------- |
| `subscription_valid`        | true while the user is in an active billing window or grace period |
| `subscription_expiry`       | the next renewal date (or final expiry if cancelled)              |
| `subscription_product_id`   | e.g. `io.devlan.rfspager.yearly`                                  |
| `subscription_platform`     | `apple` / `google` / `stripe`                                     |
| `will_renew`                | whether auto-renew is currently on                                |
| `cancellation_reason`       | `refund`, `revoked`, `grace_period_expired` (Apple only)          |
| `last_store_sync_at`        | bumped on every webhook                                           |

**User-row lookup:**

- Apple finds the user by `apple_original_transaction_id` (set at first
  successful purchase validation, never changes for that user).
- Google finds the user by `google_purchase_token` (the same token Play
  returns for every renewal of the same subscription).

If a webhook arrives for a transaction we don't have on file (e.g. the
purchase came in but the app crashed before we validated and stored the
token), the update affects zero rows — the row gets repaired the next time
the user opens the app and we run a `/iap/restore`.

---

## 1. Apple — App Store Server Notifications v2

### What it is

Apple sends a JWS-signed POST to your webhook URL every time something
material changes about a subscription. We use **version 2** (the modern
format that ships `signedTransactionInfo` + `signedRenewalInfo` in the
payload).

### Set the URL

**App Store Connect → Apps → RFSPager → App Information → App Store Server Notifications**

| Field                       | Value                                           |
| --------------------------- | ----------------------------------------------- |
| Production Server URL       | `https://<your-prod-host>/api/webhooks/apple`   |
| Production Server Version   | **Version 2** (required — we don't parse v1)    |
| Sandbox Server URL          | `https://<your-prod-host>/api/webhooks/apple`<sup>†</sup> |
| Sandbox Server Version      | **Version 2**                                   |

<sup>†</sup> The same host is fine — the JWS payload includes an
`environment` field (`Sandbox`/`Production`). Or use a different host for
sandbox if you want to keep the channels separate.

Save. Apple immediately starts firing test pings — check the server logs
for `apple webhook received`.

### Notification types we react to

Apple sends ~20 notification types; the handler currently treats them as
follows:

| `notificationType`         | Subtype          | Effect on user                                  |
| -------------------------- | ---------------- | ----------------------------------------------- |
| `SUBSCRIBED`               | initial / resub  | `valid=true`, `expires=<future>`                |
| `DID_RENEW`                | —                | `valid=true`, `expires` advanced                |
| `EXPIRED`                  | —                | `valid=false`                                   |
| `REVOKE`                   | —                | `valid=false`, `cancellation_reason=revoked`    |
| `REFUND`                   | —                | `valid=false`, `cancellation_reason=refund`     |
| `GRACE_PERIOD_EXPIRED`     | —                | `valid=false`, `cancellation_reason=grace_period_expired` |
| `DID_FAIL_TO_RENEW`        | (any)            | `valid=true` if Apple is still retrying (`expirationIntent==2`) — keeps the user in until `GRACE_PERIOD_EXPIRED` |
| `DID_CHANGE_RENEWAL_STATUS`| `AUTO_RENEW_DISABLED` | `will_renew=false`                         |

Everything else is logged but doesn't change the user row (the next
`/iap/restore` will reconcile if needed).

> **Why the grace handling matters.** If you flip the user off on the
> first `DID_FAIL_TO_RENEW`, they lose access while Apple is still trying
> to charge their card — that's almost always a transient bank decline
> and the renewal succeeds within hours. Wait for `GRACE_PERIOD_EXPIRED`.

### Required env vars

```bash
APPLE_BUNDLE_ID=io.devlan.rfspager
# Used to verify JWS payloads on inbound webhooks (the bundle ID inside
# the JWS must match):
APPLE_KEY_ID=...
APPLE_ISSUER_ID=...
APPLE_PRIVATE_KEY="-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----"
```

The `APPLE_*_KEY` triplet is **also** the App Store Server API key used
by the fallback `/iap/restore` flow. Generate it once at
**App Store Connect → Users and Access → Integrations → In-App Purchase
→ Generate API Key** with the "App Manager" role.

### Verify

1. **Apple's "Request a Test Notification" button** sits next to the
   URL field. Click it, then `tail -f` the server logs:

   ```text
   apple webhook received  type=TEST  subtype=
   ```

2. **Notification history** lives at the same screen and lets you replay
   any notification Apple has tried to deliver in the last 180 days.
   Useful when you've fixed a bug and want to backfill missed events.

3. **Force a real event** in sandbox: install the dev-client build,
   subscribe with a sandbox account, then either cancel the subscription
   in the Settings app (`DID_CHANGE_RENEWAL_STATUS`) or wait for it to
   renew (sandbox 1-month → 5 min, 1-year → 1 hr).

### Common gotchas

- **404 on Apple's first call** — the host has to be HTTPS with a real
  TLS cert. Self-signed won't work. Apple does not retry indefinitely;
  if it can't reach you, the notification is buffered for ~6 hours then
  dropped (visible in history as a delivery failure).
- **`unauthorized` in our logs** — Apple rotates its signing certs.
  We pin Apple's root CA G3 (`internal/iap/apple_root_ca_g3.pem`); if
  Apple cuts a new root we'd need to add it.
- **Bundle ID mismatch** — usually a dev build pointed at the prod
  webhook with a different bundle ID. Check `APPLE_BUNDLE_ID` matches
  the build that's purchasing.

---

## 2. Google — Real-Time Developer Notifications (RTDN)

Google's flavour is more involved because the channel between Play and
your server goes via **Cloud Pub/Sub**. Play publishes a message to a
topic; Pub/Sub pushes it to a webhook subscription on the topic. The
server validates the OIDC token on the push, decodes the Pub/Sub envelope,
then calls the Play Developer API to fetch authoritative state for the
purchase token.

### Step 1 — Pub/Sub topic + service account

In the **Google Cloud Console** (same project that owns the Play
service-account key from `docs/ci-secrets.md`):

1. **Pub/Sub → Topics → Create topic.**
   - Topic ID: `play-developer-notifications` (the name is arbitrary —
     just remember it).
   - Don't add a schema.
2. **IAM → Add member.**
   - Member: `google-play-developer-notifications@system.gserviceaccount.com`
     (this is a fixed Play-owned principal — exactly the same email for
     every developer).
   - Role: **Pub/Sub Publisher** on the topic you just made.
3. **Pub/Sub → Subscriptions → Create subscription** on that topic.
   - Subscription ID: `play-notifications-rfspager` (or similar).
   - Delivery type: **Push**.
   - Endpoint URL: `https://<your-prod-host>/api/webhooks/google`.
   - **Enable authentication:** yes.
   - Service account: pick (or create) one — e.g.
     `play-pubsub-pusher@<project>.iam.gserviceaccount.com`. This is the
     identity Pub/Sub uses to mint the OIDC token attached to each push.
   - **Audience:** leave blank to default to the endpoint URL (which is
     what we expect — see env vars below) or set explicitly to the same
     URL.
   - Ack deadline: 60 s.
   - Retry policy: exponential backoff (default).

### Step 2 — Tell Play Console where to publish

**Play Console → All apps → RFSPager → Monetisation setup**

- Topic name: `projects/<gcp-project-id>/topics/play-developer-notifications`
  (the full resource name, not just the short ID).
- Save.

Play immediately fires a `testNotification`. Look in the server logs for:

```text
google webhook received  package=io.devlan.rfspager  type=0  subscription=  message_id=...
```

(`type=0` is the test notification. Real subscription events use
`subscriptionNotification.notificationType` values 1-13 — see table
below.)

### Step 3 — Server env vars

```bash
GOOGLE_PLAY_PACKAGE=io.devlan.rfspager

# The service-account JSON used to call the Play Developer API to fetch
# the actual purchase state when a notification arrives:
GOOGLE_PLAY_CREDENTIALS=/path/to/play-service-account.json

# Pub/Sub OIDC validation — required, the handler refuses to process
# notifications if AUDIENCE is empty:
GOOGLE_PUBSUB_AUDIENCE=https://<your-prod-host>/api/webhooks/google
# Required when RTDN is enabled — pin to the exact service account that
# Pub/Sub will use. Without this, any OIDC token signed for the audience
# would be accepted.
GOOGLE_PUBSUB_SERVICE_EMAIL=play-pubsub-pusher@<project>.iam.gserviceaccount.com
```

> The Play API service account (`GOOGLE_PLAY_CREDENTIALS`) is the same
> one you set up in `docs/ci-secrets.md` for `eas submit`. The
> Pub/Sub-pusher service account is a separate identity used only for
> push auth.

### Notification types we react to

Google's `notificationType` is a small integer. The handler currently
re-validates with the Play API on every notification that carries a
purchase token, so the *effect* on the user row is identical regardless
of which event triggered it — we trust Play's current view, not the
event name. Reference table for log triage:

| `notificationType` | Name (Play docs)                          |
| ------------------ | ----------------------------------------- |
| 1                  | `SUBSCRIPTION_RECOVERED`                  |
| 2                  | `SUBSCRIPTION_RENEWED`                    |
| 3                  | `SUBSCRIPTION_CANCELED`                   |
| 4                  | `SUBSCRIPTION_PURCHASED`                  |
| 5                  | `SUBSCRIPTION_ON_HOLD`                    |
| 6                  | `SUBSCRIPTION_IN_GRACE_PERIOD`            |
| 7                  | `SUBSCRIPTION_RESTARTED`                  |
| 8                  | `SUBSCRIPTION_PRICE_CHANGE_CONFIRMED`     |
| 9                  | `SUBSCRIPTION_DEFERRED`                   |
| 10                 | `SUBSCRIPTION_PAUSED`                     |
| 11                 | `SUBSCRIPTION_PAUSE_SCHEDULE_CHANGED`     |
| 12                 | `SUBSCRIPTION_REVOKED`                    |
| 13                 | `SUBSCRIPTION_EXPIRED`                    |

After validation, the handler sets `subscription_valid`,
`subscription_expiry`, `will_renew` and `subscription_product_id` based
on what Play returns:

- `valid = true` while `expiryTimeMillis > now` AND `paymentState ∈ {1 (received), 2 (free trial), 3 (pending deferred upgrade)}` — that covers grace, on-hold, and live subscriptions.
- `will_renew` = `autoRenewing`.

If validation itself fails (e.g. the token has aged out of Google's
retention) we still return 200 OK to avoid Pub/Sub retry storms — the
user gets reconciled the next time they hit `/iap/restore`.

### Verify

1. **Click "Send test notification"** at the bottom of the Pub/Sub topic
   setup in Play Console. Server logs should show
   `type=0  subscription=`.
2. **Pub/Sub → Subscriptions → metrics tab** — every push attempt is
   counted here, with ack/nack rates. If you see `nack` climbing it
   means the server is returning 4xx/5xx and Pub/Sub is retrying.
3. **Real event in sandbox:** there isn't a Play sandbox per se — use
   the **License Testing → License responders** under Play Console to
   add an internal-test gmail account, then make a real subscription on
   a test build. Cancel from the Play Store app → expect a
   `SUBSCRIPTION_CANCELED` event within seconds.

### Common gotchas

- **`webhook not configured` (503)** — `GOOGLE_PUBSUB_AUDIENCE` is
  unset. The handler refuses to process without it, deliberately,
  because that's the only thing standing between us and someone
  forwarding arbitrary Pub/Sub payloads to our URL.
- **All notifications return 401** — the OIDC token's `aud` doesn't
  match what we expect, or you set `GOOGLE_PUBSUB_SERVICE_EMAIL` but
  Pub/Sub is signing with a different SA. Check Pub/Sub subscription
  authentication settings and confirm the audience matches the
  endpoint URL exactly (with or without trailing slash — pick one).
- **`testNotification` works, real events don't** — Play needs the
  `androidpublisher.subscriptions.get` permission on the Play API
  service account. Re-check the Play Console permissions for the
  `GOOGLE_PLAY_CREDENTIALS` service account.
- **Pub/Sub delivery delay growing** — your endpoint is timing out or
  returning errors. Pub/Sub's ack deadline is 60 s; the handler should
  complete in <1 s. Check for slow DB queries.

---

## 3. Bringing it together

When everything is wired:

- **First purchase** — app calls `/iap/apple/validate` or
  `/iap/google/validate`, server validates with the store, writes
  `apple_original_transaction_id` / `google_purchase_token`, flips
  `subscription_valid=true`.
- **Renewal one month later** — Apple/Google fire a webhook. Server
  updates `subscription_expiry` by 30 days. User never even opens the
  app.
- **User cancels** — webhook arrives within seconds. `will_renew` flips
  to `false` but `subscription_valid` stays `true` until the paid period
  actually ends.
- **Card declines** — Apple: `DID_FAIL_TO_RENEW` (we keep them valid
  during the retry window), then either `DID_RENEW` (problem resolved)
  or `GRACE_PERIOD_EXPIRED` (we flip them off). Google: same shape but
  the events are `SUBSCRIPTION_ON_HOLD` → `SUBSCRIPTION_IN_GRACE_PERIOD`
  → `SUBSCRIPTION_EXPIRED`.
- **Refund** — `REFUND` (Apple) or `SUBSCRIPTION_REVOKED` (Google).
  Immediately invalidates the subscription with a `cancellation_reason`
  set.

The **fallback safety net** is `/iap/restore`: any time the app boots
or the user opens the subscription screen, it re-asks the stores for
the truth and reconciles. This means even if a webhook is lost (Apple
or Pub/Sub retention expires, our handler 500s, etc.) the user state
heals within one app session.

> Migrating off Adapty? Run [`./cmd/adapty-backfill`](./adapty-backfill.md)
> once so the existing paid users have `apple_original_transaction_id` /
> `google_purchase_token` populated *before* the webhooks start firing —
> otherwise renewal events will land with `rows=0` until each user opens
> the app.

---

## 4. Quick sanity check

After wiring both, run this on a known test user:

```sql
SELECT id, email, subscription_valid, subscription_platform,
       subscription_product_id, subscription_expiry, will_renew,
       last_store_sync_at, cancellation_reason
FROM users
WHERE id = <your test user id>;
```

If `last_store_sync_at` updates within seconds of toggling the
subscription in the store, the loop is closed.
