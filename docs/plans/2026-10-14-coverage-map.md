# Coverage map (admin)

An admin page at `/admin/coverage` that shows where our receivers are, which
paging transmitters they can hear, and a heat map of the incidents whose pages
reached us over the last 7 / 30 / 90 / 120 days. Focus is RFS (148.5875 MHz);
FRNSW (148.9625 MHz) is selectable but secondary.

## Why it needs inference

POCSAG does not say which transmitter sent a page. What we do know:

- Each page's capcode belongs to an agency, and RFS agencies are districts.
  Unknown capcodes still carry a district prefix (`1370xxx` = Northern
  Beaches).
- RFS pages end in the incident's coordinates (`... - [150.85,-34.66]`).
- ACMA's register lists every licensed transmitter on both paging channels:
  416 RFS-owned sites plus a 36-site NSW Telco Authority overlay on 148.5875,
  and 34 NSWTA sites on 148.9625. One licence per site, so ACMA does not
  group sites by district.

A district's pages go out on that district's transmitters, so a receiver that
heard district D heard one of D's sites. We pick the most likely one with a
simple link model and say so in the UI ("probable", "possible").

## Data

Migration `034_coverage_map.sql`:

- `systems.located_since TIMESTAMPTZ NULL`: receptions before it are not
  attributed to the receiver's current location (system 3 was the Narrabeen
  PagerMon feed before it became Newcastle). NULL = always been here.
- `agencies.coverage_district BOOLEAN NOT NULL DEFAULT true`: false for
  agencies that are not RFS districts (FRNSW, SES, Volunteer Rescue
  Association, Marine Rescue, TEST, RFSPager.com, Rubish, NSWRFS, RFS). Only
  district agencies label transmitters.
- `paging_transmitters`: one row per ACMA transmitter device.
  `id, sdd_id (unique), channel ('RFS'|'FRNSW'), freq_hz, licence_no,
  licensee, acma_site_id, name, lat, lng, elevation_m NULL, height_m NULL,
  eirp_w NULL, authorised_on DATE NULL, agency_id NULL (district),
  district_source ('auto'|'manual'), district_confidence REAL NULL,
  imported_at, created_at, updated_at`.
- `page_receptions`: one row per receiver per posted page, recorded at ingest
  **before** dedupe, so every receiver that heard a page is counted.
  `id, system_id (FK systems ON DELETE CASCADE), heard_at, address,
  capcode_id NULL, lat NULL, lng NULL, incident_key NULL, damaged BOOL,
  source ('live'|'logs'|'messages')`. Index `(heard_at)` and
  `(system_id, heard_at)`; unique `(system_id, address, heard_at)` so
  backfill and recording are idempotent.
  The migration backfills **90 days**, only when the table is empty:
  `system_logs` page lines with `raw->>'posted' = 'true'` (every receiver,
  last 7 days), then each system's `messages` older than its own earliest
  such log line (one row per page, credited to the receiver whose copy won
  the dedupe). Pages with no letters in the body (numeric beacons such as
  FRNSW's 0180111 clock tick) are skipped here and at ingest.
  Coordinates come from `messages.lat/lng` or the body regex
  `\[(1[0-9]{2}\.[0-9]+),(-[0-9]{2}\.[0-9]+)\]` (lng, lat).
  Rows older than 400 days are pruned daily.

District and channel are resolved at query time, not stored, so capcode edits
apply retroactively: capcode -> agency; an unknown address takes the agency
that all (or >= 80% of) known capcodes sharing its 4-digit prefix belong to.
Channel is FRNSW when the agency is FRNSW, otherwise RFS.

## ACMA import

`internal/importer/acma.go`: downloads `ACMA_RRL_URL` (default
`https://web.acma.gov.au/rrl-updates/spectra_rrl.zip`, ~70 MB) to a temp
file, streams `site.csv`, `licence.csv`, `client.csv` and the 390 MB
`device_details.csv`, keeping `DEVICE_TYPE = 'T'` rows at 148587500 or
148962500 Hz whose licence is Granted and whose licensee is NSW Rural Fire
Service or NSW Government Telecommunications Authority. Upserts by `sdd_id`,
deletes rows not seen (keeping nothing stale), preserves `agency_id` when
`district_source = 'manual'`. Runs on boot when the table is empty or the
last import is older than 7 days, then checks daily. `ACMA_IMPORT_DISABLED=1`
turns it off.

## District assignment (transmitters)

Recomputed after each import and nightly, for `district_source = 'auto'`
rows on the RFS channel. FRNSW transmitters all get the FRNSW agency.

Labelled points, district agencies only:

1. RFS brigade stations (`emergency_stations` category `rfs`) whose
   normalised name matches exactly one capcode alias that contains
   "Brigade" (text before " Brigade", upper-cased, punctuation stripped;
   "A/B Brigade" tries "A/B", "A" and "B"). Station names that occur more
   than once are skipped. Weight 3.
2. Geocoded incidents from `messages` in the last 365 days, de-duplicated
   to one point per ~1 km cell per agency. Weight 1.

For each transmitter, sum `weight / (1 + d_km)^2` per agency over points
within 30 km (60 km if none within 30). The winner becomes `agency_id`,
`district_confidence` = winner share of the total. No points = NULL
(unmapped). Admins can override per transmitter (sets `manual`), or reset
to auto.

## Probable transmitter model

For receiver R (with lat/lng; rows with `heard_at >= located_since`) and
district D heard by R in the window with at least 3 distinct pages **to
known capcodes**:

- `score_db = 10*log10(eirp_w or 50) - 35*log10(max(d_km, 1))`
- radio horizon `4.12 * (sqrt(h_tx) + sqrt(30))` km with
  `h_tx = coalesce(elevation_m, 0) + coalesce(height_m, 10)`;
  beyond 1.3x the horizon: -20 dB.
- the best-scoring D transmitter is **probable**; others within 6 dB of it
  (max 3) are **possible**.

Transmitter status in the response, in precedence order: `probable`,
`possible`, `district_heard` (district heard in the window by anyone,
including non-attributable rows), `not_heard` (mapped district, no pages),
`unmapped` (no district).

A "page" is counted once per `(address, minute)` so the same page from
several receivers or a re-send in the same minute counts once.

Only pages to known capcodes count as hearing a district. Pages to unknown
addresses that the prefix map places in a district are reported separately
(`unknown_capcode_pages`) and never drive links or transmitter status:
verified against prod data, garbled noise decodes from the PagerMon era
landed under Monaro, Far South Coast and Riverina prefixes and produced
380 km "links" from Budgewoi.

## API

All under the admin JWT.

`GET /api/v1/admin/coverage?days=30&system_id=&channel=RFS`

- `days`: 1..365, default 30. `system_id`: optional systems.id; filters
  everything to that receiver. `channel`: `RFS` (default) | `FRNSW` | `all`.
- Cached in memory 5 minutes per parameter set.

```json
{
  "window": {"days": 30, "since": "2026-09-14T...Z", "until": "2026-10-14T...Z"},
  "receptions_since": "2026-07-16T...Z",
  "generated_at": "2026-10-14T...Z",
  "total_pages": 14815,
  "receivers": [
    {"id": 2, "name": "Budgewoi", "lat": -33.23, "lng": 151.56,
     "located_since": null, "pages": 812, "districts_heard": 7,
     "reach_km": 61.2, "last_heard_at": "2026-10-14T...Z"}
  ],
  "transmitters": [
    {"id": 17, "channel": "RFS", "name": "Mt Sugarloaf ...", "licensee": "NSW RURAL FIRE SERVICE",
     "lat": -32.9, "lng": 151.5, "eirp_w": 331, "height_m": 15, "elevation_m": 410,
     "agency_id": 7, "agency": "Lower Hunter", "district_source": "auto",
     "district_confidence": 0.92, "status": "probable",
     "heard_by": [2, 3], "pages": 140}
  ],
  "links": [
    {"system_id": 2, "transmitter_id": 17, "kind": "probable",
     "agency_id": 7, "pages": 140, "distance_km": 38.4, "score_db": -40.1}
  ],
  "districts": [
    {"agency_id": 7, "agency": "Lower Hunter", "channel": "RFS", "pages": 900,
     "unknown_capcode_pages": 35,
     "receivers": [{"system_id": 2, "pages": 700}],
     "capcodes_heard": 40, "capcodes_known": 95, "transmitters": 12,
     "last_heard_at": "2026-10-13T...Z"}
  ],
  "unmapped_prefixes": [{"prefix": "0950", "pages": 97}],
  "heat": [[151.77, -32.74, 3]]
}
```

- `receivers`: approved, non-deleted systems that have lat/lng (all of them
  when `system_id` is empty), with stats over the window.
- `districts`: every agency with `coverage_district` (plus FRNSW when the
  channel includes it), including ones with zero pages in the window;
  `last_heard_at` is all-time, from `messages`, so long silences
  (Northern Beaches since 2026-09-09) stand out. Sorted by pages desc.
- `heat`: `[lng, lat, weight]`, rounded to 2 decimals (~1 km); weight is
  distinct incidents (`incident_key`, else address+minute) in the cell.

`POST /api/v1/admin/coverage/transmitters/refresh`: starts an ACMA import in
the background, 202 `{"status": "started"}` or 409 if one is running.

`PUT /api/v1/admin/coverage/transmitters/{id}` body
`{"agency_id": 7}` sets a manual district; `{"agency_id": null}` sets it
unmapped manually; `{"auto": true}` returns it to auto and recomputes.
Invalidates the coverage cache.

Systems: existing `PUT /api/v1/admin/systems/{id}` already accepts
`lat`, `lng`, `located_since`.

## UI

- `/admin/coverage` (sidebar "Coverage"): MapLibre with the OpenFreeMap
  positron style used by the public map.
  - Layers (toggleable): heat (incidents heard), transmitters (circles
    coloured by status, size by EIRP), receivers (brand-red markers with
    names), links (receiver -> probable solid, possible dashed).
  - Controls: window 7 / 30 / 90 / 120 days, receiver (all or one),
    channel (RFS / FRNSW).
  - Side panel: district table (pages, receivers, capcodes heard/known,
    last heard; flagged when last heard > 14 days ago). Selecting a
    district highlights its transmitters and fits the map to them.
  - Transmitter popup: name, licensee, EIRP, height, district + source,
    status, heard by, and a district override select (agencies) with
    "reset to auto".
  - A footnote: transmitter attribution is modelled; receiver pins are
    suburb-level; per-receiver data before `receptions_since` is only the
    receiver whose copy was stored.
- Systems list, detail and edit pages show and edit `lat`, `lng` and
  `located_since`.

## Receiver locations (suburb level only)

Rounded to 2 decimals on purpose: the mobile systems endpoint exposes them.
Budgewoi -33.23, 151.56; Newcastle -32.93, 151.75 (the site is ~20 km away
at Raymond Terrace, so its attribution is rougher); Figtree -34.43, 150.86;
Hornsby and Hornsby 2 -33.70, 151.10. System 3 `located_since` is the start
of the Newcastle receiver's traffic.
