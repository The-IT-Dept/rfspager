#!/usr/bin/env bash
# Merge a duplicate capcode row into the one that should survive.
#
#   scripts/capcode-merge.sh KEEP_ID DROP_ID [--alias "new alias"] [--apply] [--target root@host]
#
# Two live rows with the same address break alerts: ingest
# (db.FindCapcodeByAddress) picks one of them for every new page, so anyone
# subscribed to the other row silently stops getting notified. This moves
# subscriptions, feed filters, messages, page_receptions, photos and capcode
# suggestions from DROP_ID to KEEP_ID (users on both keep one row), soft-deletes
# DROP_ID and optionally renames KEEP_ID. Relinking messages sends no pushes:
# the only trigger on messages is AFTER INSERT.
#
# Without --apply it runs inside a transaction and rolls back, so you can read
# the before/after first. Find duplicates with scripts/capcode-audit.py.
set -euo pipefail

TARGET="root@100.64.244.122"
APPLY=false
ALIAS=""
ARGS=()
while (($#)); do
  case "$1" in
    --apply) APPLY=true ;;
    --alias) ALIAS="$2"; shift ;;
    --target) TARGET="$2"; shift ;;
    -h|--help) sed -n '2,17p' "$0"; exit 0 ;;
    *) ARGS+=("$1") ;;
  esac
  shift
done
if [[ ${#ARGS[@]} -ne 2 || ! ${ARGS[0]} =~ ^[0-9]+$ || ! ${ARGS[1]} =~ ^[0-9]+$ || ${ARGS[0]} == "${ARGS[1]}" ]]; then
  echo "usage: $0 KEEP_ID DROP_ID [--alias \"new alias\"] [--apply] [--target root@host]" >&2
  exit 2
fi
KEEP="${ARGS[0]}"
DROP="${ARGS[1]}"

ssh "$TARGET" 'cd /tmp && sudo -u postgres psql -d rfspager -X -q -v ON_ERROR_STOP=1' <<SQL
\set keep $KEEP
\set drop $DROP
\set alias '${ALIAS//\'/\'\'}'
\set apply $APPLY
\pset pager off
begin;
select count(*) = 1 as ok from capcodes k join capcodes d on d.address = k.address
 where k.id = :keep and d.id = :drop and k.deleted_at is null and d.deleted_at is null \gset
\if :ok
\else
  \echo 'KEEP_ID and DROP_ID must both be live capcodes with the same address'
  select 1/0;
\endif

\echo == before ==
select c.id, c.address, c.alias, c.agency,
  (select count(*) from subscriptions s where s.capcode_id = c.id) subs,
  (select count(*) from filters f where f.capcode_id = c.id) filters,
  (select count(*) from messages m where m.capcode_id = c.id) messages,
  (select max(created_at) from messages m where m.capcode_id = c.id) last_page
from capcodes c where c.id in (:keep, :drop) order by c.id = :keep desc;

insert into subscriptions (user_id, capcode_id, created_at, updated_at)
  select user_id, :keep, created_at, now() from subscriptions where capcode_id = :drop
  on conflict (user_id, capcode_id) do nothing;
delete from subscriptions where capcode_id = :drop;
insert into filters (user_id, capcode_id, created_at, updated_at)
  select user_id, :keep, created_at, now() from filters where capcode_id = :drop
  on conflict (user_id, capcode_id) do nothing;
delete from filters where capcode_id = :drop;
update messages set capcode_id = :keep where capcode_id = :drop;
update page_receptions set capcode_id = :keep where capcode_id = :drop;
update photos set capcode_id = :keep where capcode_id = :drop;
update capcode_suggestions set capcode_id = :keep where capcode_id = :drop;
update capcodes set deleted_at = now(), updated_at = now() where id = :drop;
update capcodes set alias = coalesce(nullif(:'alias', ''), alias), updated_at = now() where id = :keep;

\echo == after ==
select c.id, c.address, c.alias, c.agency, c.deleted_at is not null as deleted,
  (select count(*) from subscriptions s where s.capcode_id = c.id) subs,
  (select count(*) from filters f where f.capcode_id = c.id) filters,
  (select count(*) from messages m where m.capcode_id = c.id) messages
from capcodes c where c.id in (:keep, :drop) order by c.id = :keep desc;

\if :apply
  commit;
  \echo applied
\else
  rollback;
  \echo 'dry run, rolled back (pass --apply to commit)'
\endif
SQL
