#!/usr/bin/env python3
"""
Read-only audit of the prod capcode table against the pages each capcode receives.

    scripts/capcode-audit.py                       # export from apps01, print report
    scripts/capcode-audit.py --cached              # re-analyse the last export
    scripts/capcode-audit.py --co-receivers        # + who else gets the unknowns' pages (slow)
    scripts/capcode-audit.py --target root@dev --days 180

Exports go to scripts/out/capcode-audit/ (git-ignored). Nothing is written to the
database; fixes are applied by hand, see docs/capcode-audit.md.

RFS pages start with the turnout group they were sent to, e.g.
"HKDO - 26-131498 - AFA - ..." or "THBOXHI1 - ...": a two-letter district code,
then DO / GO / G5 / FCO2 / OPS10 for officer groups or a five-letter brigade
abbreviation (BOXHI = Box Hill, ROUHI = Rouse Hill) plus a unit suffix. The
prefix names the group, not the pager, which is what makes these checks work.
"""
import argparse
import collections
import csv
import os
import difflib
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
csv.field_size_limit(sys.maxsize)

TS = re.compile(r"^(\d{1,2} \w+ \d{4} )?(\d\d:\d\d:\d\d )?(- )?")
PFX = re.compile(r"^([A-Z][A-Z0-9]{1,11}) - \d\d-\d{6}")
STOP_PFX = re.compile(r"^\d\d-\d{6} ([A-Z][A-Z0-9]{1,11}) Stop")
OFFICER = re.compile(r"^..(DO|GO|DDO|RDO|DG\d*|G\d+|FCO\d*|OPS\d*|HQ.*|CMD.*|CFR\d*|BW|BULK)$")

# Capcodes looked at by hand whose pages legitimately cover more than the
# brigade in their name (a pager on two neighbouring brigades' groups).
REVIEWED = {
    "0570112": "Avoca: Copacabana + Avoca Beach groups",
    "0570144": "Brisbane Waters: mostly Erina",
    "0720064": "Canoelands: mostly Maroota",
    "1170256": "Putty: Howes Valley + CFR30",
    "1370432": "West Pittwater: Elvina",
    "0570256": "Mooney = Mooney Mooney (MOOMO)",
    "0570208": "MacMasters = MacMasters Beach (MACBE)",
    "1440104": "Werombi = Orangeville/Werombi (ORAWE)",
    "0690368": "Hawkesbury Headquarters (HAWHE)",
    "0690632": "Hawkesbury Headquarters (HAWHE)",
}

# Alias words checked by hand: real place names that merely look like a typo
# of some other word in the pages.
OK_WORDS = {"COBAR", "FINLEY", "JUNEE", "MACLEAN", "YENDA", "LIDSTER", "TORRINGTON", "SALLY",
            "SHOOTER", "FIFIELD", "CURRARONG", "TECHS", "RUBISH"}

STRIP_TS = r"regexp_replace(message, '^(\d{1,2} \w+ \d{4} )?(\d\d:\d\d:\d\d )?(- )?', '')"
EXPORTS = {
    "caps": r"""
        select c.id, c.address, c.alias, c.agency, a.agency as agency_name, c.icon, c.colour,
               (select count(*) from subscriptions s where s.capcode_id = c.id) as subs,
               (select count(*) from filters f where f.capcode_id = c.id) as filters
        from capcodes c left join agencies a on a.id = c.agency_id
        where c.deleted_at is null""",
    # Latest 300 pages per capcode. ORDER BY deleted_at DESC, created_at DESC
    # makes the planner use messages(capcode_id, deleted_at, created_at);
    # created_at DESC alone walks the whole feed index (minutes per capcode).
    "prof": r"""
        select c.id, mm.created_at, mm.message
        from capcodes c join lateral (
          select message, created_at from messages
          where capcode_id = c.id and deleted_at is null
          order by deleted_at desc, created_at desc limit 300) mm on true
        where c.deleted_at is null""",
    "unreg": rf"""
        select address, substring({STRIP_TS} from '^([A-Z][A-Z0-9]{{1,11}}) - \d\d-\d{{6}}') as p,
               count(*) as n, max(created_at) as last
        from messages
        where deleted_at is null and capcode_id is null and created_at > now() - interval '{{days}} days'
        group by 1, 2""",
    "dangling": r"""
        select d.id, d.address, d.alias,
               (select count(*) from subscriptions s where s.capcode_id = d.id) as subs,
               (select count(*) from filters f where f.capcode_id = d.id) as filters
        from capcodes d
        where d.deleted_at is not null
          and (exists (select 1 from subscriptions s where s.capcode_id = d.id)
               or exists (select 1 from filters f where f.capcode_id = d.id))""",
    # For every Unknown?? capcode: named capcodes that got the identical page
    # (timestamp stripped) within 3 minutes of it.
    "co": rf"""
        with u as (select id, address from capcodes where deleted_at is null and alias like 'Unknown%'),
        um as (select u.id uid, u.address, mm.id mid, mm.created_at, {STRIP_TS.replace('message', 'mm.message')} body
               from u join lateral (select id, message, created_at from messages where capcode_id = u.id and deleted_at is null
                                    order by deleted_at desc, created_at desc limit 200) mm on true)
        select um.address, c.address as co_address, c.alias as co_alias, count(distinct um.mid) as shared,
               (select count(*) from um x where x.uid = um.uid) as sampled
        from um join messages m2 on m2.deleted_at is null and m2.capcode_id is not null and m2.capcode_id <> um.uid
             and m2.created_at between um.created_at - interval '3 min' and um.created_at + interval '3 min'
             and {STRIP_TS.replace('message', 'm2.message')} = um.body
        join capcodes c on c.id = m2.capcode_id
        group by um.uid, um.address, c.address, c.alias""",
}


def export(name, target, out, days):
    sql = " ".join(EXPORTS[name].replace("{days}", str(days)).split())
    path = out / f"{name}.csv"
    cmd = ["ssh", target, "cd /tmp && sudo -u postgres psql -d rfspager -X -q -v ON_ERROR_STOP=1"]
    with open(path, "w") as f:
        subprocess.run(cmd, input=f"\\copy ({sql}) to stdout csv header\n", text=True, stdout=f, check=True)
    print(f"exported {name}: {sum(1 for _ in csv.DictReader(open(path)))} rows", file=sys.stderr)


def rows(out, name):
    path = out / f"{name}.csv"
    return list(csv.DictReader(open(path))) if path.exists() else []


def prefix(message):
    body = TS.sub("", message, count=1)
    m = PFX.match(body) or STOP_PFX.match(body)
    return m.group(1) if m else None


def core(p):
    """THBOXHI1 -> BOXHI, LHWYEE7 -> WYEE: the brigade part of a prefix."""
    m = re.match(r"[A-Z]+", p[2:])
    return m.group(0)[:5] if m else ""


STOPWORDS = {"brigade", "captain", "brigades", "rfs", "bfb", "deputy", "senior", "and"}


def abbreviations(alias):
    words = [w.upper() for w in re.sub(r"[^A-Za-z/ ]", " ", alias).replace("/", " ").split()
             if w.lower() not in STOPWORDS]
    words = [{"MOUNT": "MT", "HTS": "HEIGHTS"}.get(w, w) for w in words]
    out = set()
    for i, w in enumerate(words):
        out.add(w[:5])
        rest = "".join(words[i + 1:])
        for k in (1, 2, 3, 4):
            out.add((w[:k] + rest)[:5])
    return out


def section(title, lines):
    print(f"\n== {title} ({len(lines)}) ==")
    for line in lines:
        print("  " + line)


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--target", default=os.environ.get("RFSPAGER_PROD_HOST", "root@apps01-syd-theitdept-au"))
    ap.add_argument("--days", type=int, default=90, help="window for unregistered addresses")
    ap.add_argument("--cached", action="store_true", help="reuse the last export")
    ap.add_argument("--co-receivers", action="store_true", help="also export co-receivers of Unknown?? capcodes")
    ap.add_argument("--out", type=Path, default=ROOT / "scripts/out/capcode-audit")
    args = ap.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)
    if not args.cached:
        (args.out / "co.csv").unlink(missing_ok=True)  # don't report a stale co-receiver export
        for name in ("caps", "prof", "unreg", "dangling") + (("co",) if args.co_receivers else ()):
            export(name, args.target, args.out, args.days)

    caps = {c["id"]: c for c in rows(args.out, "caps")}
    pfx_all = collections.defaultdict(collections.Counter)
    pfx_recent = collections.defaultdict(collections.Counter)
    last = {}
    vocab = collections.Counter()
    newest = max((r["created_at"] for r in rows(args.out, "prof")), default="")
    year_ago = f"{int(newest[:4]) - 1}{newest[4:10]}" if newest else ""
    for r in rows(args.out, "prof"):
        cid = r["id"]
        last[cid] = max(last.get(cid, ""), r["created_at"][:10])
        for w in re.findall(r"[A-Z]{4,}", r["message"].upper()):
            vocab[w] += 1
        p = prefix(r["message"])
        if p:
            pfx_all[cid][p] += 1
            if r["created_at"] >= year_ago:
                pfx_recent[cid][p] += 1
    fmt = lambda c, n=6: " ".join(f"{p}:{k}" for p, k in c.most_common(n))
    label = lambda c: f"{c['address']} {c['alias']!r} [{c['agency']}]"

    print(f"capcodes: {len(caps)}, with traffic: {len(last)}, newest page: {newest[:16]}")

    # --- structure ---
    by_addr = collections.defaultdict(list)
    for c in caps.values():
        by_addr[c["address"]].append(c)
    section("DUPLICATE ADDRESSES: ingest picks one row, the other's subscribers get nothing; merge with capcode-merge.sh", [
        f"{a}: " + " ; ".join(f"id {c['id']} {c['alias']!r} [{c['agency']}] subs={c['subs']} last={last.get(c['id'], '-')}" for c in cs)
        for a, cs in sorted(by_addr.items()) if len(cs) > 1])
    section("SUBSCRIPTIONS/FILTERS ON DELETED CAPCODES", [
        f"id {d['id']} {d['address']} {d['alias']!r} subs={d['subs']} filters={d['filters']}" for d in rows(args.out, "dangling")])
    section("ALIAS WHITESPACE", [label(c) for c in caps.values() if c["alias"] != re.sub(r"\s{2,}", " ", c["alias"].strip())])
    section("AGENCY TEXT ≠ agencies.agency", [f"{label(c)} agency_id → {c['agency_name']!r}" for c in caps.values() if c["agency"] != c["agency_name"]])
    section("ICON NOT 'fas fa-*' OR COLOUR NOT LOWERCASE", [
        f"{label(c)} icon={c['icon']!r} colour={c['colour']!r}" for c in caps.values()
        if not re.fullmatch(r"fas fa-[a-z-]+", c["icon"]) or c["colour"] != c["colour"].strip().lower()])

    # --- unknowns ---
    lines = []
    for c in sorted(caps.values(), key=lambda c: (c["agency"], c["address"])):
        if not c["alias"].lower().startswith("unknown"):
            continue
        prof = pfx_all[c["id"]]
        tot = sum(prof.values())
        do = sum(n for p, n in prof.items() if p.endswith("DO"))
        kind = ""
        if tot:
            brigades = {core(p) for p in prof if not OFFICER.match(p)}
            kind = "DO only" if do / tot > 0.8 else "DO + all brigades (all calls)" if do and len(brigades) > 4 else ""
        lines.append(f"{label(c)} last={last.get(c['id'], 'never')} subs={c['subs']} {kind} | {fmt(prof)}")
    section("UNKNOWN?? CAPCODES (prefix = turnout group; DO-only → '<District> Duty Officer', DO + every brigade → '<District> All Calls')", lines)

    co = collections.defaultdict(list)
    for r in rows(args.out, "co"):
        co[r["address"]].append(r)
    if co:
        section("UNKNOWN?? CO-RECEIVERS (named capcodes that got the same page)", [
            f"{a} ({rs[0]['sampled']} sampled): " + " ; ".join(
                f"{r['co_address']} {r['co_alias']} [{r['shared']}]"
                for r in sorted(rs, key=lambda r: -int(r["shared"]))[:6]) for a, rs in sorted(co.items())])

    unreg = collections.defaultdict(collections.Counter)
    unreg_last = {}
    for r in rows(args.out, "unreg"):
        if r["p"]:
            unreg[r["address"]][r["p"]] += int(r["n"])
            unreg_last[r["address"]] = max(unreg_last.get(r["address"], ""), r["last"][:10])
    section(f"UNREGISTERED ADDRESSES WITH ≥20 TURNOUT PAGES IN {args.days} DAYS (registering puts them in every feed)", [
        f"{a} last={unreg_last[a]} {'DO ' if any(p.endswith('DO') for p in c) else ''}| {fmt(c)}"
        for a, c in sorted(unreg.items(), key=lambda x: -sum(x[1].values())) if sum(c.values()) >= 20])

    # --- districts ---
    agency_codes = collections.defaultdict(collections.Counter)
    for cid, prof in pfx_all.items():
        for p, n in prof.items():
            agency_codes[caps[cid]["agency"]][p[:2]] += n
    allowed = {a: {k for k, n in c.items() if n >= 0.1 * sum(c.values())} for a, c in agency_codes.items()}
    section("DISTRICT CODES PER AGENCY (≥10% of its pages; two agencies sharing one code = RFS merged them)", [
        f"{a}: {' '.join(sorted(s))}" for a, s in sorted(allowed.items())])
    lines = []
    for cid, prof in pfx_recent.items():
        codes = collections.Counter()
        for p, n in prof.items():
            codes[p[:2]] += n
        top, n = codes.most_common(1)[0]
        if n >= 3 and n >= 0.5 * sum(codes.values()) and top not in allowed.get(caps[cid]["agency"], {top}):
            lines.append(f"{label(caps[cid])} mostly {top} | {fmt(prof)}")
    section("CAPCODE IN THE WRONG AGENCY (last 12 months)", sorted(lines))

    # --- brigades ---
    lines, reviewed = [], 0
    for cid, prof in pfx_recent.items():
        c = caps[cid]
        al = c["alias"].lower()
        if "brigade" not in al or re.search(r"support|catering|comms|group|all calls|bulk", al):
            continue
        cores = collections.Counter()
        for p, n in prof.items():
            if not OFFICER.match(p) and not p.startswith("VR"):
                cores[core(p)] += n
        if not cores:
            continue
        top, n = cores.most_common(1)[0]
        if n >= 3 and top not in abbreviations(c["alias"]):
            if c["address"] in REVIEWED:
                reviewed += 1
            else:
                lines.append(f"{label(c)} gets {top} | {fmt(prof)}")
    section(f"BRIGADE NAME ≠ TURNOUT GROUP (last 12 months; usually a merged or renamed brigade; "
            f"{reviewed} in REVIEWED skipped)", sorted(lines))

    # --- FRNSW ---
    lines = []
    for c in caps.values():
        if c["agency"] != "FRNSW":
            continue
        m = re.match(r"\s*(\d+)", c["alias"])
        a = re.fullmatch(r"01(\d{3})11", c["address"])
        if not m or not a:
            lines.append(f"{label(c)} non-standard alias or address")
        elif int(m.group(1)) != int(a.group(1)):
            lines.append(f"{label(c)} address says station {int(a.group(1))}")
    section("FRNSW (alias station number must match address 01NNN11)", lines)

    # --- spelling ---
    known = {w for w, n in vocab.items() if n >= 3}
    skip = {"BRIGADE", "CAPTAIN", "GROUP", "OFFICER", "OFFICERS", "SUPPORT", "CATERING", "COMMS", "DEPUTY", "DISTRICT"}
    lines = []
    for c in caps.values():
        for w in {w.upper() for w in re.findall(r"[A-Za-z]{4,}", c["alias"])} - known - skip - OK_WORDS:
            for m in difflib.get_close_matches(w, known, n=1, cutoff=0.85):
                if vocab[m] >= 10 and m[0] == w[0] and abs(len(m) - len(w)) <= 2:
                    lines.append(f"{label(c)} {w} → {m}? (seen {vocab[m]}×)")
    section("POSSIBLE TYPOS (alias word vs place names in pages; review, many are fine)", sorted(lines))


if __name__ == "__main__":
    main()
