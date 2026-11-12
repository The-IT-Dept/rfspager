# Capcode audit

How to check that every capcode's name, agency and row still match the pages
it actually receives, and how to fix what the check finds. First done by hand
on 2026-10-16; the scripts below repeat it.

| Script | What it does |
|---|---|
| `scripts/capcode-audit.py` | Read-only. Exports capcodes and recent pages from apps01 over ssh and prints a report. |
| `scripts/capcode-merge.sh` | Merges a duplicate capcode row into the one that should survive. Dry run unless `--apply`. |

```sh
scripts/capcode-audit.py                    # export from apps01 (~20 s) and report
scripts/capcode-audit.py --cached           # re-analyse scripts/out/capcode-audit/ without re-exporting
scripts/capcode-audit.py --co-receivers     # + which named capcodes get the same pages as each Unknown??
scripts/capcode-audit.py --target root@dev   # the dev box instead
```

Run it after any bulk capcode import, after RFS district restructures, and
whenever a user says a brigade name looks wrong.

## How the checks work

RFS pages begin with the **turnout group** the page was sent to:

```
HKDO - 26-131498 - AFA - INCIDENT CALL - ...      Hornsby/Ku-Ring-Gai, Duty Officer group
THBOXHI1 - 26-118685 - AFA - ...                  The Hills, Box Hill brigade, unit 1
16 October 2026 13:19:07 HKDO - 26-131498 - ...   same, with a leading timestamp (stripped first)
26-126769 HKDO Stop Message // ...                stop messages put the group after the incident number
```

Two letters of district, then either an officer group (`DO`, `GO`, `G5`, `DG4`,
`FCO2`, `OPS10`, `HQ1`) or a five-letter brigade abbreviation (`BOXHI` Box Hill,
`ROUHI` Rouse Hill, `MTKEI` Mt Keira, `COOGA` Cooks Gap) with a unit suffix
(`1`, `7A`, `P`, `BT`, `PC`). The prefix names the **group**, not the pager: every
capcode in the group gets the identical page. So a capcode's mix of prefixes
says what it is:

- only `XXDO` pages plus district broadcasts → `<District> Duty Officer`
- `XXDO` **and** every brigade's prefix → a district officer / all-calls pager → `<District> All Calls`
- one brigade's prefix → that brigade (`... Brigade`, or `... Brigade Captain` if it also gets the captains' broadcasts)

In some districts the DO group also reaches the group officers (Hornsby's HKDO goes
to Group 2/3/4/6, Group Officers and Brooklyn Captain), so a DO-only capcode
there can also be a group officer's pager. The data can't tell them apart, so
call it Duty Officer.

FRNSW pages are different (`FRINC TYPE: AFA TURNOUT: 474 INC: ...`). There the
alias's station number has to match the address (`01` + station + `11`).

## Report sections and fixes

**Duplicate addresses**: highest priority. `db.FindCapcodeByAddress` picks one
of the live rows for every new page, so subscribers of the other row get no
alerts. After the Oct 6 Go cutover it switched to the newer row and stranded
about 300 subscribers across 0107911, 0123711, 0140211, 0147411 and 1310144
until 2026-10-16. Decide which row is right (the page content usually says so),
then:

```sh
scripts/capcode-merge.sh KEEP_ID DROP_ID --alias "237 Branxton"           # dry run: before/after, rolled back
scripts/capcode-merge.sh KEEP_ID DROP_ID --alias "237 Branxton" --apply
```

**Subscriptions/filters on deleted capcodes**: those users get nothing. Move
them to the live row for the same address, or delete them if nothing replaces it.

**Unknown?? capcodes**: name them from the prefix mix as above. With
`--co-receivers`, a capcode that gets the same pages as, say, `0180064 Group
Officer` is probably in the same role. Rows that have never received a page
can't be named. Rename with SQL (examples below).

**Unregistered addresses**: addresses with turnout pages but no capcode row.
Registering one puts its pages, past and future, in **every user's feed** (known
capcodes are visible to everyone), so check with Nick first. Insert the row, then
link the history so it shows under the new name:

```sql
update messages set capcode_id = <new id> where address = '<address>' and capcode_id is null;
update page_receptions set capcode_id = <new id> where address = '<address>' and capcode_id is null;
```

This sends no pushes, because the only trigger on `messages` is `AFTER INSERT`.

**District codes per agency / wrong agency**: two agencies sharing one code
means RFS merged the districts (as of 2026-10: Macarthur pages use Cumberland's
`CM`, Sutherland uses Illawarra's `IS`). We haven't merged the agencies, because
that regroups the capcode picker. A capcode whose pages are mostly another
district's code belongs in that agency: set `agency_id` (the
`trg_sync_capcode_agency` trigger rewrites the `agency` text).

**Brigade name ≠ turnout group**: usually a brigade that merged or was renamed
(2026-10-16: Leppington → Catherine Field, Varroville and Minto Heights →
Georges River, Lemon Tree Passage → Tilligerry, Millfield → Paxton). Rename it
to the brigade it actually gets. If the pager legitimately covers two
neighbouring brigades, or the abbreviation just doesn't match the name
(MacMasters = `MACBE`), add the address to `REVIEWED` in the script so it stops
being reported.

**Possible typos**: alias words that are close to a place name seen in pages.
Real place names that only look like typos go in `OK_WORDS`.

## Renaming by hand

```sql
begin;
update capcodes set alias = 'Catherine Field Brigade', updated_at = now()
 where deleted_at is null and address = '0280688' and alias = 'Leppington Brigade';   -- guard on the old alias
-- check the row count, then
commit;
```

Use the district's existing colour and an icon matching its siblings:

| Role | Icon |
|---|---|
| Brigade | `fas fa-fire` |
| Captain | `fas fa-user-circle` |
| Officers / DO / all calls | `fas fa-briefcase` |
| Broadcast | `fas fa-bullhorn` |
| SES | `fas fa-medkit` |
| VRA | `fas fa-car` |

The apps strip `fas fa-` before looking up the icon, so a trailing space breaks
it. Colours are lowercase CSS names.

## Things to know

- Ingest looks the capcode up live for every page (no cache), so edits apply to
  the next page. The admin coverage report caches capcodes for 15 minutes.
- Querying messages per capcode: order by `deleted_at DESC, created_at DESC` so
  Postgres uses `messages(capcode_id, deleted_at, created_at)`. `created_at DESC`
  alone walks the feed index and takes minutes per capcode on prod.
- The SES daily-test capcodes say their unit in the text (`BINGARA DAILY TEST`),
  which is how `Bindara SES` was caught.
