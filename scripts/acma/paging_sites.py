"""Find paging transmitter sites in the ACMA RRL offline dataset.

usage: python3 -I paging_sites.py <data_dir>
"""
import csv
import math
import sys
from collections import defaultdict

csv.field_size_limit(1 << 30)
D = sys.argv[1].rstrip("/") + "/"

TARGETS = {148587500: "RFS 148.5875", 148962500: "FRNSW 148.9625"}
BAND = (148_000_000, 149_000_000)
REF = {
    "Raymond Terrace": (-32.7600, 151.7440),
    "Budgewoi": (-33.2330, 151.5600),
    "Figtree": (-34.4330, 150.8600),
}


def km(a, b):
    la1, lo1 = map(math.radians, a)
    la2, lo2 = map(math.radians, b)
    h = math.sin((la2 - la1) / 2) ** 2 + math.cos(la1) * math.cos(la2) * math.sin((lo2 - lo1) / 2) ** 2
    return 2 * 6371 * math.asin(math.sqrt(h))


sites = {}
with open(D + "site.csv", newline="", encoding="utf-8", errors="replace") as f:
    for r in csv.DictReader(f):
        try:
            sites[r["SITE_ID"]] = (float(r["LATITUDE"]), float(r["LONGITUDE"]), r["NAME"], r["STATE"], r["POSTCODE"], r.get("ELEVATION", ""))
        except (ValueError, KeyError):
            continue

licences = {}
with open(D + "licence.csv", newline="", encoding="utf-8", errors="replace") as f:
    for r in csv.DictReader(f):
        licences[r["LICENCE_NO"]] = (r["CLIENT_NO"], r["LICENCE_CATEGORY_NAME"], r["STATUS_TEXT"], r["DATE_OF_EXPIRY"])

clients = {}
with open(D + "client.csv", newline="", encoding="utf-8", errors="replace") as f:
    for r in csv.DictReader(f):
        clients[r["CLIENT_NO"]] = (r["LICENCEE"], r["TRADING_NAME"])

band_rows = []
with open(D + "device_details.csv", newline="", encoding="utf-8", errors="replace") as f:
    for r in csv.DictReader(f):
        try:
            fq = int(float(r["FREQUENCY"]))
        except ValueError:
            continue
        if BAND[0] <= fq <= BAND[1] and r.get("DEVICE_TYPE", "") == "T":
            band_rows.append((fq, r))


def describe(r):
    lic = licences.get(r["LICENCE_NO"], ("", "", "", ""))
    cl = clients.get(lic[0], ("?", ""))
    s = sites.get(r["SITE_ID"])
    power = f'{r.get("EIRP","")}{r.get("EIRP_UNIT","")}' if r.get("EIRP") else f'{r.get("TRANSMITTER_POWER","")}{r.get("TRANSMITTER_POWER_UNIT","")}'
    return lic, cl, s, power


print("=" * 100)
for fq, label in TARGETS.items():
    rows = [r for f, r in band_rows if f == fq]
    print(f"\n### {label} MHz: {len(rows)} transmitter registrations nationally")
    by_lic = defaultdict(int)
    nsw = []
    for r in rows:
        lic, cl, s, power = describe(r)
        by_lic[(cl[0], lic[1], lic[2])] += 1
        if s and s[3] == "NSW":
            nsw.append((km(REF["Raymond Terrace"], s[:2]), km(REF["Budgewoi"], s[:2]), km(REF["Figtree"], s[:2]), s[2], s[0], s[1], cl[0], power, r.get("EMISSION", ""), r.get("HEIGHT", ""), lic[2], r["LICENCE_NO"], s[5]))
    print("licensees (count, licensee, category, status):")
    for k, n in sorted(by_lic.items(), key=lambda x: -x[1])[:12]:
        print(f"  {n:4d}  {k[0][:45]:45s} {k[1][:32]:32s} {k[2]}")
    print(f"\nNSW sites nearest Raymond Terrace (km RT / km Budgewoi / km Figtree):")
    print(f"  {'RT':>6} {'Bud':>6} {'Fig':>6}  {'site':42s} {'lat':>9} {'lon':>10}  {'licensee':30s} {'eirp/pwr':>9} {'emission':9} {'agl':>4} {'elev':>5} status")
    for row in sorted(nsw)[:22]:
        print(f"  {row[0]:6.1f} {row[1]:6.1f} {row[2]:6.1f}  {row[3][:42]:42s} {row[4]:9.4f} {row[5]:10.4f}  {row[6][:30]:30s} {row[7]:>9} {row[8]:9} {row[9]:>4} {row[12]:>5} {row[10]}")

print("\n" + "=" * 100)
print("\n### All 148-149 MHz transmitters within 60 km of Raymond Terrace (any licensee)")
near = []
for fq, r in band_rows:
    lic, cl, s, power = describe(r)
    if not s:
        continue
    d = km(REF["Raymond Terrace"], s[:2])
    if d <= 60:
        near.append((fq / 1e6, d, s[2], cl[0], power, r.get("EMISSION", ""), lic[2]))
print(f"  {'MHz':>9} {'km':>6}  {'site':42s} {'licensee':34s} {'eirp/pwr':>9} emission  status")
for row in sorted(near):
    print(f"  {row[0]:9.4f} {row[1]:6.1f}  {row[2][:42]:42s} {row[3][:34]:34s} {row[4]:>9} {row[5]:9} {row[6]}")

print("\n" + "=" * 100)
print("\n### Rural Fire Service transmitters (any frequency) within 80 km of Raymond Terrace")
rfs = []
with open(D + "device_details.csv", newline="", encoding="utf-8", errors="replace") as f:
    for r in csv.DictReader(f):
        if r.get("DEVICE_TYPE", "") != "T":
            continue
        lic = licences.get(r["LICENCE_NO"])
        if not lic:
            continue
        cl = clients.get(lic[0], ("", ""))
        name = (cl[0] + " " + cl[1]).lower()
        if "rural fire" not in name and "rfs" not in name.split():
            continue
        s = sites.get(r["SITE_ID"])
        if not s:
            continue
        d = km(REF["Raymond Terrace"], s[:2])
        if d <= 80:
            try:
                fq = int(float(r["FREQUENCY"])) / 1e6
            except ValueError:
                continue
            rfs.append((d, fq, s[2], cl[0], r.get("EMISSION", ""), r.get("EIRP", "") + r.get("EIRP_UNIT", ""), lic[2]))
print(f"  {'km':>6} {'MHz':>9}  {'site':42s} {'licensee':34s} emission  eirp   status")
for row in sorted(rfs)[:60]:
    print(f"  {row[0]:6.1f} {row[1]:9.4f}  {row[2][:42]:42s} {row[3][:34]:34s} {row[4]:9} {row[5]:>7} {row[6]}")
print(f"\n({len(rfs)} RFS transmitter registrations within 80 km)")
