# Go cutover + Adapty-exit audit — 2026-09-29

> **Resolution (same day):** the sharing fix landed — see §9. Dev (`rfspager` on
> 157.20.113.123) has migrations 027–029 applied and the new Go binary deployed;
> the live 430 frozen accounts were re-linked.

## 9. Sharing fix — done

- `internal/db/migrations/029_share_iap_entitlements.sql`: entitlement is 1:N.
  Composite-PK link table, non-unique `users` store-ID indexes, and the 430
  accounts 023 froze are rebuilt from the `reconcile:ownership_released` audit
  rows (no Adapty re-run).
- `internal/iap/subscription.go`: validate/restore just link the caller (no more
  409); webhooks fan out to every linked account but only overwrite an account
  whose *current* pointer is that identifier (or the Google token being
  upgraded), so a stale event can't clobber a newer purchase.
- `cmd/adapty-backfill`: now upserts the ledger link, so prod's
  migrate-then-backfill order is safe and re-runnable.
- New-app client: `/iap/restore` `restored`→`valid` normalisation, expo-iap v4
  kebab-case cancel code, static `getTransactionJwsIOS` import.
- Rehearsed on a full copy of the dev DB (store-ID bindings 1,258→1,688;
  valid-but-unbound 204→8; 349 shared identifiers; no dup/owner gaps), then Go
  integration + migration suites green against real Postgres.

Remaining before prod: P1 items in §7 (Apple `will_renew`, Google `CANCELED`
handling, Stripe events) and the P0 #2/#3 items above.

Audit across the Go backend, `laravel-old`, `app-old` (production mobile, Adapty +
GraphQL) and `apps/mobile` (new Expo app, Go REST). Goal: assess how close we are
to (a) moving serving traffic to Go and (b) removing Adapty, while Laravel keeps
serving un-updated mobile clients over GraphQL.

Method: static inspection of routes, handlers, migrations, store SDK calls and
docs; `go build ./...` passes. No live providers exercised.

---

## 1. Verdict

**Go is substantially feature-complete for new clients, but cutover is NOT ready.**
The plumbing to run Go and Laravel side-by-side against one database is sound
(shared `installations`/`notifications` tables, bcrypt-compatible passwords,
Laravel-format notification rows). The blockers are concentrated in the
Adapty-exit path and the new app's IAP client.

| Workstream | State | Ready? |
|---|---|---|
| Go: auth, messages, capcodes, alertzones, incidents, systems, admin, ingress, receiver WS | Implemented, tested, builds | Yes |
| Go: direct store IAP (Apple/Google/Stripe + webhooks) | Implemented, signature-verified | Mostly |
| Adapty exit / old-app→Go billing handoff | Manual one-shot backfill with a conflict bug | **No** |
| New app IAP (client) | Several functional bugs | **No** |
| DB migration preflight | Duplicate/timestamp hazards unresolved | Rehearse |
| Coexistence (importers/notifications/receivers) | Needs deliberate per-system cutover | Plan |

Rough effort to be cutover-safe: **weeks, not days** — the IAP pieces are small but
must be proven against stores before anyone relies on them.

---

## 2. Is the "Laravel keeps serving old clients" plan viable?

Architecturally yes:

- Same `users` table; Go only adds columns (`004`, `022`, `028`), which Laravel
  ignores.
- Both hash with bcrypt (`internal/mobile/auth.go:162`, `laravel-old/config/hashing.php:18`).
- Installations/notifications are shared and format-compatible: Go inserts
  `App\Notifications\PagerNotification` rows (`internal/notify/notify.go:105`).
- Old app talks Sanctum GraphQL to Laravel; new app talks JWT REST to Go.

But three coexistence hazards must be handled:

1. **Do not run both importer loops.** Go starts major-incident + hazard importers
   (`internal/importer/importer.go:52`) and Laravel schedules the same
   (`laravel-old/app/Console/Kernel.php`). Both upsert `upstream_id`; `002` aborts
   on duplicates. Disable one set or stagger.
2. **Do not run both notification workers on the same ingested system.** Go
   `NotifyForMessage` and Laravel `SendMessageNotificationListener` both fire for a
   message. Whoever ingests notifies — so as receivers/systems migrate, the
   `ImportMessages` pull for those systems must stop on Laravel.
3. **Receivers:** Go removed the legacy UUID/Sanctum ingress
   (`internal/handler/router.go:7`). At cutover each receiver must be re-provisioned
   with a Go key; the embedded historical credentials are all retired.

---

## 3. IAP audit (primary focus)

### 3.1 Where each side stands

| Component | Mechanism | Store IDs written? | Notes |
|---|---|---|---|
| `app-old` | Adapty SDK (`react-native-adapty`) paywall `rfspager-main`, access level `premium` | No | `app-old/app/screens/subscription/subscription-screen.tsx:72,207`; `components/subscription-modal.tsx:56,89` |
| Laravel webhook | Adapty → `POST /api/adapty-webhook` | **No** | Writes only `subscription_*`/`adapty_id`/`will_renew` (`laravel-old/app/Http/Controllers/IAPController.php:51-59`) |
| Laravel sync cmd | Adapty server API | **No** | `laravel-old/app/Actions/Users/SyncWithAdapty.php:31-43` |
| `apps/mobile` | direct StoreKit2 JWS / Play token → Go | Go writes them | `apps/mobile/src/services/purchase-service.ts` |
| Go backfill | Adapty server API → users row | Yes (if Adapty has it) | `cmd/adapty-backfill/main.go` |
| Go webhooks | Apple S2S v2 / Google RTDN | match on store ID | `internal/iap/webhook.go` |

**The handoff only exists through the manual backfill.** Laravel never populated
`apple_original_transaction_id` / `google_purchase_token` / `subscription_platform`,
and Go webhooks only match rows that have them
(`internal/iap/subscription.go:180,188`). Until a user is backfilled or opens the
new app and hits `/iap/restore`, their renewals/refunds are invisible to Go.

### 3.2 P0 blockers

1. **~~Backfill re-violates migration 023's unique indexes.~~ RESOLVED.**
   Duplicate accounts (one person, several addresses) are the norm, not an
   anomaly, so entitlements are now **shared 1:N**. Migration
   `029_share_iap_entitlements.sql` turns `iap_subscription_ownership` into a
   link table (PK `platform, original_transaction_id, user_id`), drops the two
   unique indexes on `users`, and re-links the 430 accounts 023 had frozen by
   rebuilding them from the `reconcile:ownership_released` audit rows. The
   backfill now upserts the ledger link and is idempotent. Rehearsed on a copy
   of the dev database: store-ID bindings restored 1,258→1,688, valid-but-unbound
   users 204→8, no dup/owner gaps, then verified by the Go integration suites.

2. **New-app restore always reports "no purchases".** Server returns
   `{restored:true,…}` (`internal/iap/handler.go:240-250`); both callers test
   `result?.valid` (`subscription-screen.tsx:207`, `subscription-modal.tsx:98`) which
   is always undefined. Startup sync correctly uses `restored`
   (`purchase-service.ts:117`).

3. **Adapty gap for users with no `store_original_transaction_id`.** Pre-late-2022
   or stale Adapty records give entitlement but no store ID
   (`docs/adapty-backfill.md:164-172`). They are unreachable by Go webhooks until they
   update the app and `/iap/restore` runs. Acceptable only if old-app users keep
   being served by Laravel+Adapty until they update — i.e. **Adapty cannot be revoked
   until the old-app population is negligible.**

### 3.3 P1 (correctness / billing divergence)

4. **Apple `will_renew` hardcoded true** on both the JWS path
   (`internal/iap/handler.go:130`) and the API fallback (`apple.go:134`). Cancelled
   subscribers read as renewing until a webhook arrives.
5. **Google `SUBSCRIPTION_STATE_CANCELED` marks users invalid immediately**
   (`internal/iap/google.go:70-71`), while Apple stays valid until expiry. A Google
   user who cancelled but is still inside the paid period loses access.
   `SUBSCRIPTION_STATE_ON_HOLD` is unhandled.
6. **New app uses obsolete cancel code** `"E_USER_CANCELLED"`
   (`purchase-service.ts:152,175`, screens), but expo-iap 4.7.2 emits `'user-cancelled'`
   (`node_modules/expo-iap/build/types.d.ts:473`). Cancel shows as an error.
7. **Fragile JWS fetch**: `getTransactionJwsIOS` is bound via a racy dynamic
   `import()` (`purchase-service.ts:21-26`); correct field is
   `purchase.purchaseToken` (documented as iOS JWS, `expo-iap/build/types.d.ts:1306`).
   Startup sync is **iOS-only**; Android never syncs.
8. **`gifted` SKU missing** from the Go allowlist default (`internal/config/config.go:84`),
   though both apps know it (`app-old/.../RootStore.ts:171`,
   `apps/mobile/src/stores/auth-store.ts:308`). A gifted store receipt is rejected.
9. **Unowned webhooks are silently dropped without an audit row.**
   `applyWebhookSubscription` returns early (`subscription.go:162-167`); the comment
   claims historical events are still recorded, but no `iap_transactions` row is
   written. Renewals arriving before backfill are lost.
10. **Backfill can write `subscription_platform='stripe'`** by bypassing the
    ownership ledger, which 023 CHECKs to only apple/google
    (`023_iap_ownership.sql:132`); those rows also lack `stripe_id`, so Go Stripe
    webhooks won't match them.
11. **Stripe gaps**: no `checkout.session.completed`, `invoice.paid`/
    `invoice.payment_failed`, no event dedupe, no unique on `stripe_id`/
    `stripe_subscription_id` (`028_stripe_user_columns.sql`), customer-create race
    (`internal/stripe/handler.go:258-276`), and the overwrite guard lets a newer
    `created` event steal an apple/google row (`:220-224`). `subscription_platform`
    isn't reset on delete (`:243-251`).

### 3.4 Apple JWS verification

Chain/signature logic is correct: 3-part JWS, ES256, x5c chain to pinned Apple Root
CA G3, SHA-256 ECDSA verify, `alg` and bundle checks
(`internal/iap/apple_verify.go:38-118`). Missing: `signedDate`/freshness, notification
UUID dedupe, `notificationType` allowlist, and cross-check of request
`transaction_id` vs JWS `transactionId`. `InAppOwnershipType`/`Type`/`OfferType`
are parsed but unused (`models.go:100-114`).

---

## 4. Feature parity — Laravel GraphQL/REST vs Go

Go mobile router: `internal/mobile/router.go:20-102`. New app endpoints all resolve
to it except one.

**Missing on Go:**
- `GET /api/v2/mobile/magic-link` — called at `apps/mobile/src/services/api-client.ts:238`,
  no server route anywhere.

**Implemented on Go but unused by the new app:**
- SSE `/events` (messages) and `/systems/{uuid}/logs` — no `EventSource` consumer;
  system logs fall back to 3s polling (`use-systems.ts:53-63`). Live message feed has
  no SSE.
- `/auth/reset-password` (client fn exists, no UI), `GET /photos` (no screen).

**Feature regressions vs `app-old`:**
- Photos: no screen; capture commented out (`pager-message-screen.tsx:284-286`).
- Reset-password UI absent in both new app and old app.

**Old app gaps that stay on Laravel until fixed:** it never calls `mutateLogout`
(no server-side token revoke), stores the token in AsyncStorage (keychain helper
unused), has no GraphQL websocket for `messageCreated`, and has no deep-link routing.
Laravel `/v2/auth/me` and `/logout` use the `web` session guard, not `auth:sanctum`
(`laravel-old/routes/api.php:41-45`).

---

## 5. DB / migration readiness

- `users` carries everything needed, incl. `adapty_id`/`last_adapty_sync_at`
  (`internal/db/migrations/000_bootstrap_legacy_schema.sql:32-33`).
- `023` ownership ledger + partial unique indexes are well-built (ranking by
  installations, audit rows, abort on unresolved duplicates) — but interact badly
  with the backfill (§3.2.1).
- `012_timestamps_to_timestamptz.sql` rewrites every timestamp column under
  `AccessExclusiveLock` held to commit — schedule it.
- `002` aborts on duplicate `upstream_id`; pre-reconcile.
- `027` deletes duplicate `responding`/`subscriptions`/`filters` rows (lowest id).
- Go migrations never create Laravel's `two_factor_*` columns, yet admin redaction
  reads them (`internal/admin/store.go:1267`). A DB cloned from the live Laravel DB
  has them; a Go-fresh DB does not.
- No migration for the legacy liap/paywall tables (factories exist, no migration).

---

## 6. Security / operational

- Good: audience-separated JWTs, token revocation, signature-verified webhooks,
  product/environment allowlists, receiver key auth, body limits, trusted-proxy
  handling, `iap_transactions` audit trail.
- Receiver credentials embedded in old history must be rotated at cutover.
- New app `.env` targets `https://go.dev.blah.au`; release builds hard-require
  `EXPO_PUBLIC_API_URL` (`apps/mobile/app.config.js:15`).
- No token refresh on Go/new app; expiry always forces full logout via 401.

---

## 7. Prioritised actions

**P0 — before any cutover:**
1. Make `cmd/adapty-backfill` ownership-conflict-aware (or re-run 023 dedupe after it);
   dry-run against a restored DB copy and confirm zero `23505`.
2. Fix new-app restore result handling (`restored`, not `valid`) and cancel-code
   string; use `purchase.purchaseToken` JWS on iOS; add Android startup sync.
3. Add `gifted` (and any Adapty paywall SKUs) to `IAP_PRODUCT_IDS`; audit Adapty
   dashboard product IDs against the allowlist.
4. Decide and document the Adapty-revocation gate: no revocation until backfill is
   clean and old-app share is negligible.
5. Rehearse migration on a disposable clone; reconcile `upstream_id` duplicates;
   time `012`.

**P1 — before Go owns billing:**
6. Apple: set real `will_renew` from renewal info; add `signedDate` freshness and
   notification dedupe.
7. Google: treat `CANCELED`/`ON_HOLD` by expiry/grace, not immediate invalid.
8. Record dropped/unowned webhooks in `iap_transactions`.
9. Stripe: add invoice/checkout events, dedupe, uniqueness on `stripe_id`, fix
   customer race and overwrite guard.
10. Backfill `stripe_id` for existing Stripe subscribers.

**P2 — parity/coexistence:**
11. Implement Go `magic-link` or drop it from the client.
12. Wire SSE (or accept polling) and restore Photos/reset-password UI.
13. Per-system importer/notification handover runbook; add `two_factor_*` to a
    migration for Go-only installs.

---

## 8. Recommended sequencing

1. Schema-only `cmd/migrate` on a clone; resolve duplicates; keep Laravel running.
2. Fix P0 client + backfill code; deploy new app build pointing at Go.
3. Backfill from Adapty; verify with a real sandbox Apple/Google renewal test that
   `rows=1` against a backfilled account.
4. Migrate receivers per-system (Go key, stop Laravel pull/importer for that system,
   confirm exactly one notification).
5. Move new clients to Go; keep Laravel GraphQL live for old clients.
6. Only revoke Adapty when old-app share is negligible and every paid user has a
   store ID or has hit `/iap/restore`.
