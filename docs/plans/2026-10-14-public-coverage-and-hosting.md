# Public coverage map, web hosting applications, application emails

Follow-up to `2026-10-14-coverage-map.md`. Three parts:

1. A public coverage map on the marketing site at `/coverage`, replacing the
   gated `/systems` page, with a "help us cover more of NSW" call to action.
2. The mobile-only "apply to host a receiver" flow, on the web at `/host`.
3. Emails for applications through the Libredesk support desk: a thank-you
   on apply (the conversation stays open) and a reply in the same
   conversation on approval with the install command. Both mobile and web
   applications use the same server path, so both get the emails.

Also: the hosting terms said "RFSPager Pty Ltd"; the entity is
**The IT Dept Pty Ltd**. Fixed in the app and used on the web. Terms
version becomes `2026-10`.

## 1. Public coverage endpoint

`GET /api/public/coverage?days=90` (no auth). `days` is 30 or 90 (anything
else → 90). Built from the admin coverage report (`internal/coverage`,
channel RFS), reduced to what is safe and useful in public:

```json
{
  "window": {"days": 90, "since": "...Z", "until": "...Z"},
  "generated_at": "...Z",
  "total_pages": 14815,
  "receivers": [{"name": "Budgewoi", "lat": -33.23, "lng": 151.56, "last_heard_at": "...Z"}],
  "transmitters": [{"lat": -32.9, "lng": 151.5, "heard": true, "district": "Lower Hunter"}],
  "districts": [{"name": "Lower Hunter", "heard": true, "pages": 2495, "last_heard_at": "...Z"}],
  "heat": [[151.77, -32.74, 3]]
}
```

- Receivers: approved, non-deleted systems with coordinates. Names only
  (no ids, owners, uuids). Coordinates rounded to 2 decimals server-side
  regardless of what is stored.
- Transmitters: RFS channel only, position + `heard` (status probable,
  possible or district_heard) + district name (null when unmapped). No
  licensee, EIRP, links or model scores. Positions are public ACMA data.
- Districts: RFS districts only (coverage_district), `heard` = pages > 0
  in the window.
- Heat: as the admin report.
- Cached in memory 15 minutes per `days`; response header
  `Cache-Control: public, max-age=900`. Lives in `internal/public` (or a
  small handler calling `coverage.Service`), registered in
  `cmd/server/main.go` with the other `/api/public` routes.

## 2. Host application emails (Libredesk)

Migration `035_system_support_conversation.sql`:
`systems.support_conversation_uuid TEXT NULL`.

`internal/mail`: a Libredesk client for receiver applications, sharing the
HTTP plumbing (`do`, auth, URL validation) with the password reset mailer.

- `SendApplicationReceived(ctx, to, name, app) (conversationUUID string, err)`
  — `POST /api/v1/conversations` (inbox, initiator agent, agent_id = bot,
  contact email, first name, subject "Thanks for offering to host an
  RFSPager receiver"), then best-effort: tag `receiver-application`, and if
  `LIBREDESK_TEAM_ID` > 0 `PUT /conversations/{uuid}/assignee/team`
  `{"assignee_id": team}`. **Status is left Open** (unlike resets, which
  are resolved). The body thanks them, repeats what they submitted (system
  name, suburb, notes), says what happens next (we review, then reply in
  this thread with the install command; they can reply here with
  questions), and links https://rfspager.app/host.
- `SendApplicationApproved(ctx, conversationUUID, to, name, systemName, installCommand) (conversationUUID string, err)`
  — if conversationUUID is set: `POST /conversations/{uuid}/messages`
  `{"message": html, "private": false, "sender_type": "agent"}`. If empty
  (applied before this shipped, or the first email failed): create a new
  conversation as above with the approval body and return its uuid. Body:
  approved, the install command in a `<pre>`, what it does (installs the
  receiver service; needs a Pi 4/5 with an RTL-SDR plugged in; takes a few
  minutes), where to see status (https://rfspager.app/host and the app),
  and that they can reply here.
- **Libredesk renders message content through text/template.** Any
  user-controlled text (name, suburb, notes, system name) must be
  HTML-escaped AND have `{` / `}` replaced with `&#123;` / `&#125;` so it
  cannot inject template actions. The install command is server-built but
  escape it the same way. Unit-test this.
- Both calls are best-effort from the caller's point of view: applying and
  approving never fail because email failed; failures are logged with
  slog.Warn.

Wiring:

- Mobile `CreateSystem` (`internal/mobile/handlers.go`): after the insert,
  in a goroutine with a detached 30 s context, send the received email to
  the user's account email/name and store the returned uuid on the row.
- Admin `ApproveSystem` (`internal/admin/handlers.go`): after the update,
  in a goroutine, send the approved email (install command via
  `install.CommandFor(r, apiKey)`, computed before the goroutine since it
  reads the request) to the owner's email, and store the uuid if a new
  conversation was created. Existing push notification unchanged.
- Mailer is optional: nil when Libredesk isn't configured (dev/tests) →
  skip silently. Config: reuse LIBREDESK_URL/API_KEY/API_SECRET/INBOX_ID/
  AGENT_ID; add optional `LIBREDESK_TEAM_ID`.
- Libredesk side (ops, done by hand): bot role gains `messages:write` and
  `conversations:update_team_assignee`; tag `receiver-application` exists.
  **Correction (found on deploy):** assigning a team makes Libredesk drop
  the user assignee, so the bot loses `read_assigned` access. The bot
  (agent 19) is therefore a member of team 5 with
  `conversations:read_team_all` and availability `away_manual`, so
  round-robin skips it. Still no `read_all`.

## 3. Web

- `/coverage` (public): page header in the style of `/pager` and `/map`,
  MapLibre map (positron, same worker workaround as `map.tsx`) with heat,
  transmitter dots (heard: brand-adjacent green/teal; not heard yet: grey),
  receiver markers (brand red, name label). Stats: receivers, districts
  heard of total, pages in the window. District list (heard / not heard
  with last heard). 30 / 90 day toggle. Loader fetches
  `/api/public/coverage` server-side (SSR) using `API_URL`, like
  `home.tsx`; the client re-fetches on toggle.
  CTA panel: "Grey dots are RFS paging sites we can't hear yet. Live near
  one? Host a receiver." → `/host`.
- `/systems` → redirect to `/coverage` (keep the route for old links).
  Nav link "Coverage" (replace the commented-out Systems placeholder) and
  footer link.
- Home page dark "We listen to the radio" section: replace the
  `ReceiverNetwork` list in the right column with a compact, non-
  interactive coverage map card (same data, SSR loader) linking to
  `/coverage`; the "Host a receiver" mailto becomes a link to `/host`.
  Keep the live ticker and everything else.
- `/host` (public page, login needed to submit):
  - Intro: what hosting means, what you need (Raspberry Pi 4/5, RTL-SDR or
    compatible dongle, antenna, power, internet), link to `/coverage`.
  - Not signed in: "Sign in to apply" → `/login?next=/host` and "Create an
    account" → `/register?next=/host` (register must honour `next`, safely,
    like login's `safeNext`).
  - Signed in: the same 3 steps as the app (Hardware checkboxes → Terms
    with an "I agree" checkbox → Details: name, suburb, notes) posting to
    `POST /api/v2/mobile/systems` via the customer API client.
    Success state: "Thanks — check your email" (the thank-you email).
  - "Your receivers": `GET /api/v2/mobile/my-systems` list with pending /
    approved status; approved ones show the `install_command` with a copy
    button.
  - Hosting terms text lives in `apps/web/app/lib/hosting-terms.ts`, a copy
    of the app's `hosting-terms.ts` with the entity fixed.
- Customer API additions in `apps/web/app/lib/customer-api.ts`:
  `createSystem`, `mySystems`.

## 4. Free Pro for hosts ("host comp")

People hosting a receiver that is online and decoding get Pro free while it
stays that way. Agreed policy (2026-10-14):

- **Qualifies:** the user owns at least one approved, non-deleted receiver
  that, over the last 72 hours, was connected for at least 90% of the time
  **and** decoded at least one posted page (`page_receptions`). A receiver
  needs 24 hours of history before it can qualify. Its window is the shorter
  of 72 hours and the time since its first stats row, but never less than
  24 hours.
- **Connected time** comes from `system_stats`: a connected receiver writes
  a row every stats interval (default 15 s). Count distinct time buckets
  with at least one row, divided by the buckets in the window. The bucket
  must be at least the largest allowed stats interval (check the receiver
  config limits in `internal/rxproto`).
- **Grace: 7 days.** While a user qualifies, a job sets
  `host_comp_until = now + 7 days` every 15 minutes. When they stop
  qualifying, it simply stops being extended and lapses 7 days later.
- **Separate from store entitlements.** Migration 036 adds
  `users.host_comp_until TIMESTAMPTZ NULL` and
  `users.host_comp_since TIMESTAMPTZ NULL` (start of the current comp
  period; NULL when not comped). The comp never writes `subscription_valid`,
  `subscription_expiry`, `subscription_product_id` or
  `subscription_platform`, and the store and expiry paths never touch the
  host columns.
- **Effective access** everywhere access is checked or reported is
  `subscription_valid OR host_comp_until > NOW()`:
  - push fan-out (`internal/dispatch`)
  - the mobile user loader and `/me`
  - Pro-feature gating in the mobile handlers
  - the web account endpoints
  - admin listings and detail, which show both

  The API keeps the `subscription_valid` field (now effective) and adds
  `host_comp_until`. When access comes only from the comp, report
  `subscription_product_id = "io.devlan.rfspager.host"` with a null
  platform, the same way gifts report `io.devlan.rfspager.gifted`.
- **Notifications on transitions only:**
  - **Start:** when `host_comp_since` goes from NULL to now.
  - **Lapse:** when `host_comp_until` has passed while `host_comp_since` is
    still set; notify, then clear `host_comp_since`.

  Each transition sends a push through the notifier and a Libredesk email.
  The email is a reply in the qualifying receiver's
  `support_conversation_uuid` thread, or a new conversation if there isn't
  one. Both are best-effort.
  - **Start copy:** thanks for hosting; Pro is on us while the receiver
    stays online. If they already pay through the App Store or Google Play
    they can cancel that, and they keep Pro while hosting.
  - **Lapse copy:** the receiver hasn't been online enough lately (it needs
    to be online about 90% of the time over three days), so free Pro has
    paused. It comes back automatically once the receiver has been steadily
    online for about three days. Reply if you need help.

### Implementation notes (host comp)

- Code: `internal/hostcomp` (policy constants, `Access`/`CompOnly` SQL
  predicates, the job), migration `036_users_host_comp.sql`, wired in
  `cmd/server/main.go` (first run 1 minute after boot, then every 15
  minutes). One runner across servers via a transaction-level advisory
  lock; the job reads and writes in one transaction and notifies after
  commit in a background goroutine with a detached 30 s timeout per
  transition.
- Window maths is in whole 5-minute buckets (`rxproto.MaxStatsIntervalS`).
  The window ends at the start of the current bucket (a partly elapsed
  bucket is never held against the receiver) and starts at the later of
  72 h before that and the start of the bucket holding the first stats row,
  so "24 hours of history" is lenient by under 5 minutes. 90% is integer
  maths (`connected * 10 >= buckets * 9`), so exactly 90% qualifies. The
  page check is any `page_receptions` row from the window start to now.
  Only receivers that write `system_stats` (Go receivers on the websocket
  hub) can qualify; legacy PagerMon-ingress receivers cannot.
- Extension uses `GREATEST(host_comp_until, now + 7 days)`, so it never
  shortens a comp. Extension runs before the lapse check, so a user whose
  comp ran out while the job was down but who still qualifies is simply
  extended (no lapse + start pair). On lapse `host_comp_until` is kept as
  the record of when it ended.
- Which receiver the email is about: start → the user's qualifying
  receiver with a support conversation, else the most recently approved;
  lapse → the user's approved receiver (deleted or not) with a thread, else
  the most recently approved. A new conversation's uuid is stored on that
  receiver only if it still has none. New conversations are tagged
  `receiver-application`, team-assigned and left Open, like applications.
- Reporting (`/api/v2/mobile/me`, login/register/social login/profile
  update, and the middleware user that gates Pro features): computed in Go
  (`AuthUser.withEffectiveAccess`) rather than SQL, so the store columns'
  types don't matter. Comp only → `subscription_valid: true`,
  `subscription_product_id: "io.devlan.rfspager.host"`,
  `subscription_platform: null`, `will_renew: null`,
  `subscription_expiry` = `host_comp_until`. A valid store subscription is
  reported as stored. `host_comp_until` is always included.
- Admin: user list/detail add `pro_access` (effective) next to the raw
  store and host columns (`u.*` already includes them); system list/detail
  add `owner_host_comp_until`; billing adds `active_host_comp`. Dashboard
  paid/free counts, billing active_* counts and MRR stay on the store slot
  (they are revenue numbers). The host columns are not admin-editable. The
  only admin path that writes them is removing a receiver's supporter, which
  ends a comp through `hostcomp.EndIfNoReceiver` (see Supporters below).
- Email copy also mentions paying "by card on our website" (Stripe) next to
  the App Store and Google Play.

### Supporters (added 2026-11-07)

A receiver can also comp people besides its owner, e.g. a brigade member who
paid for half the gear. They are its **supporters**, listed in
`system_supporters` (migration 039) and managed from the *Supporters* card on
the admin system page (`GET/POST /api/v1/admin/systems/{id}/supporters`,
`DELETE …/supporters/{userId}`, superadmin only). Adding takes an account
email and a note (up to 1000 characters). The exact address wins over
accounts whose address differs only in case, as does the only match;
otherwise the add is refused as ambiguous. The owner can't be added, and
neither can anyone to a receiver with no owner, since the job only measures
receivers that have one.

- **Same rule as the owner.** The job measures the receiver once and treats
  each supporter like the owner for it: same 90%/72 h + decoding test, same
  7-day grace, same start/lapse transitions. Someone who owns a qualifying
  receiver and supports another is emailed about their own.
- **Their own thread.** A supporter's emails never go into the receiver's
  `support_conversation_uuid` thread. That thread is the owner's application,
  and it holds the install command and API key. The first email starts a new
  conversation, and its uuid is stored in
  `system_supporters.conversation_uuid`.
- **Lapse is always a supporter lapse.** The email is about a receiver they
  support, in their own thread or a new one, even if it is no longer
  approved. Deleting the owner's account revokes approval, and the supporter
  should not get a host's "check your Pi" email. An approved receiver is
  preferred over one that never was.
- **Copy:** start thanks them for supporting the receiver; Pro is on them
  too while it stays online, and store payers can cancel and keep it. Lapse:
  the receiver hasn't been online enough, so free Pro has paused and comes
  back by itself. Neither sends them to the host page or the Pi. The lapse
  push is the owner's (it names the receiver either way).
- **Removing** someone ends their comp at once and without a lapse email,
  unless they still own or support an approved receiver. In that case the job
  keeps deciding it. Removal takes the job's advisory lock (`hostcomp.Lock`),
  so a running job can't re-extend the comp from the row being deleted. The
  row is deleted outright; only a log line records the removal.
