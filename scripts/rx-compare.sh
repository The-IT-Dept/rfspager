#!/usr/bin/env bash
#
# Compare what two receivers decoded over the same window, from the page and
# stats rows they ship to prod (system_logs, system_stats). Read-only.
#
#   scripts/rx-compare.sh 2 6                          # last 1 hour
#   scripts/rx-compare.sh 2 6 '2026-10-14 00:32:20+00'  # since a point in time
#
# Pages are paired per transmission: same channel and address, within ±5 s.
# A page is "clean" when it has no unrepairable codewords and is not
# truncated. RFS sites re-send a page 30-60 s apart, so each re-send counts as
# its own transmission.
set -euo pipefail

A="${1:?usage: $0 <system_a> <system_b> [since]}"
B="${2:?usage: $0 <system_a> <system_b> [since]}"
SINCE="${3:-}"
HOST="${RX_COMPARE_HOST:-root@100.64.244.122}"

[[ "$A" =~ ^[0-9]+$ && "$B" =~ ^[0-9]+$ ]] || { echo "system ids must be numeric" >&2; exit 2; }

ssh -o BatchMode=yes "$HOST" \
  "sudo -u postgres psql -d rfspager -X -q -P pager=off -v a=$A -v b=$B -v since=$(printf %q "${SINCE:-}") -f -" <<'SQL'
\set ON_ERROR_STOP on
SELECT coalesce(nullif(:'since', '')::timestamptz, now() - interval '1 hour') AS t0 \gset

\echo
SELECT id, name, to_char(:'t0'::timestamptz AT TIME ZONE 'Australia/Sydney', 'YYYY-MM-DD HH24:MI') AS since_local,
       round(extract(epoch FROM now() - :'t0'::timestamptz) / 3600, 2) AS hours
  FROM systems WHERE id IN (:a, :b) ORDER BY id;

CREATE TEMP TABLE p AS
SELECT l.id, l.system_id AS sys, l.ts,
       l.raw->>'channel' AS ch, l.raw->>'address' AS addr, l.raw->>'msg' AS msg,
       coalesce((l.raw->>'corrected')::int, 0) AS corr,
       coalesce((l.raw->>'uncorrectable')::int, 0) AS unc,
       coalesce((l.raw->>'truncated')::bool, false) AS trunc,
       (l.raw->>'address' = '0521839') AS beacon
  FROM system_logs l
 WHERE l.system_id IN (:a, :b) AND l.message = 'page' AND l.ts >= :'t0';

\echo '== Pages per receiver and channel (beacon excluded)'
SELECT ch, sys, count(*) AS pages,
       count(*) FILTER (WHERE unc = 0 AND NOT trunc) AS clean,
       count(*) FILTER (WHERE unc > 0) AS w_unrepairable,
       count(*) FILTER (WHERE trunc) AS truncated,
       count(*) FILTER (WHERE corr > 0) AS needed_repair,
       round(avg(corr), 1) AS avg_corrected
  FROM p WHERE NOT beacon GROUP BY 1, 2 ORDER BY 1, 2;

\echo '== FRNSW beacon: minutes with an intact beacon'
SELECT sys, count(DISTINCT date_trunc('minute', ts)) FILTER (WHERE msg ~ '^[A-Za-z0-9+/]{60}$') AS intact_minutes,
       round(extract(epoch FROM now() - :'t0'::timestamptz) / 60) AS minutes_in_window
  FROM p WHERE beacon GROUP BY 1 ORDER BY 1;

-- Pair transmissions: each A page with the nearest B page on the same
-- channel+address within 5 s, and vice versa for B-only.
CREATE TEMP TABLE pr AS
SELECT a.ch, a.addr, a.ts, a.id AS a_id, b.id AS b_id
  FROM p a
  LEFT JOIN LATERAL (
    SELECT b.id FROM p b
     WHERE b.sys = :b AND b.ch = a.ch AND b.addr = a.addr AND abs(extract(epoch FROM b.ts - a.ts)) <= 5
     ORDER BY abs(extract(epoch FROM b.ts - a.ts)) LIMIT 1) b ON true
 WHERE a.sys = :a AND NOT a.beacon
UNION ALL
SELECT b.ch, b.addr, b.ts, NULL, b.id
  FROM p b
 WHERE b.sys = :b AND NOT b.beacon
   AND NOT EXISTS (SELECT 1 FROM p a WHERE a.sys = :a AND a.ch = b.ch AND a.addr = b.addr
                    AND abs(extract(epoch FROM b.ts - a.ts)) <= 5);

\echo '== Transmissions heard: both / only A / only B, and who decoded better when both did'
SELECT pr.ch,
       count(*) FILTER (WHERE a_id IS NOT NULL AND b_id IS NOT NULL) AS both,
       count(*) FILTER (WHERE b_id IS NULL) AS only_a,
       count(*) FILTER (WHERE a_id IS NULL) AS only_b,
       count(*) FILTER (WHERE pa.msg = pb.msg) AS identical_text,
       count(*) FILTER (WHERE pa.unc + pa.trunc::int < pb.unc + pb.trunc::int
                           OR (pa.unc + pa.trunc::int = pb.unc + pb.trunc::int AND pa.corr < pb.corr)) AS a_better,
       count(*) FILTER (WHERE pb.unc + pb.trunc::int < pa.unc + pa.trunc::int
                           OR (pa.unc + pa.trunc::int = pb.unc + pb.trunc::int AND pb.corr < pa.corr)) AS b_better
  FROM pr LEFT JOIN p pa ON pa.id = pr.a_id LEFT JOIN p pb ON pb.id = pr.b_id
 GROUP BY 1 ORDER BY 1;

\echo '== RFS transmissions side by side (A | B; [corrected/unrepairable, T=truncated])'
SELECT to_char(pr.ts AT TIME ZONE 'Australia/Sydney', 'HH24:MI:SS') AS t, pr.addr,
       CASE WHEN pa.id IS NULL THEN '—' ELSE format('[%s/%s%s] %s', pa.corr, pa.unc, CASE WHEN pa.trunc THEN ' T' ELSE '' END, left(pa.msg, 70)) END AS a,
       CASE WHEN pb.id IS NULL THEN '—' ELSE format('[%s/%s%s] %s', pb.corr, pb.unc, CASE WHEN pb.trunc THEN ' T' ELSE '' END, left(pb.msg, 70)) END AS b
  FROM pr LEFT JOIN p pa ON pa.id = pr.a_id LEFT JOIN p pb ON pb.id = pr.b_id
 WHERE pr.ch = 'RFS' ORDER BY pr.ts;

\echo '== Signal (stats intervals with carrier on the channel)'
SELECT s.system_id AS sys, c->>'label' AS ch, count(*) AS intervals,
       round(avg((c->>'snr_db')::numeric) FILTER (WHERE (c->>'carrier_pct')::numeric > 0), 1) AS snr_with_carrier,
       round(avg((c->>'noise_floor_dbfs')::numeric), 1) AS floor_dbfs,
       sum((c->>'sync_locks')::int) AS locks, sum((c->>'cw_clean')::int) AS cw_clean,
       sum((c->>'cw_corrected')::int) AS cw_corr, sum((c->>'cw_uncorrectable')::int) AS cw_unrep
  FROM system_stats s, jsonb_array_elements(s.stats->'channels') c
 WHERE s.system_id IN (:a, :b) AND s.ts >= :'t0'
 GROUP BY 1, 2 ORDER BY 2, 1;

\echo '== Dongle / DSP'
SELECT system_id AS sys, max(stats->'dongle'->>'sample_rate') AS rate,
       bool_or((stats->'dongle'->>'agc')::bool) AS agc,
       round(avg((stats->'dongle'->>'dsp_load')::numeric), 2) AS dsp_avg,
       round(max((stats->'dongle'->>'dsp_load')::numeric), 2) AS dsp_max,
       sum((stats->'dongle'->>'iq_dropped')::int) AS iq_dropped,
       sum((stats->'dongle'->>'gaps')::int) AS gaps,
       round(max((stats->'host'->>'cpu_temp_c')::numeric), 1) AS max_temp,
       bool_or((stats->'host'->>'undervoltage_now')::bool) AS undervolt
  FROM system_stats WHERE system_id IN (:a, :b) AND ts >= :'t0'
 GROUP BY 1 ORDER BY 1;
SQL
