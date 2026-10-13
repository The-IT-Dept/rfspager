"""Everything licensed at the Raymond Terrace exchange, the NSWTA paging
overlay's authorisation dates, and anything near 148.500 MHz.

usage: python3 -I raymond_terrace.py <data_dir>
"""
import csv
import math
import sys
from collections import defaultdict

csv.field_size_limit(1 << 30)
D = sys.argv[1].rstrip("/") + "/"
RT = (-32.7600, 151.7440)


def km(a, b):
    la1, lo1 = map(math.radians, a)
    la2, lo2 = map(math.radians, b)
    h = math.sin((la2 - la1) / 2) ** 2 + math.cos(la1) * math.cos(la2) * math.sin((lo2 - lo1) / 2) ** 2
    return 2 * 6371 * math.asin(math.sqrt(h))


sites = {}
with open(D + "site.csv", newline="", encoding="utf-8", errors="replace") as f:
    for r in csv.DictReader(f):
        try:
            sites[r["SITE_ID"]] = (float(r["LATITUDE"]), float(r["LONGITUDE"]), r["NAME"], r["STATE"])
        except (ValueError, KeyError):
            continue
licences = {}
with open(D + "licence.csv", newline="", encoding="utf-8", errors="replace") as f:
    for r in csv.DictReader(f):
        licences[r["LICENCE_NO"]] = (r["CLIENT_NO"], r["LICENCE_CATEGORY_NAME"], r["STATUS_TEXT"], r["DATE_ISSUED"], r["DATE_OF_EXPIRY"])
clients = {}
with open(D + "client.csv", newline="", encoding="utf-8", errors="replace") as f:
    for r in csv.DictReader(f):
        clients[r["CLIENT_NO"]] = r["LICENCEE"]

near_sites = {sid: s for sid, s in sites.items() if km(RT, s[:2]) <= 3.0}
print("### sites within 3 km of Raymond Terrace")
for sid, s in sorted(near_sites.items(), key=lambda x: km(RT, x[1][:2])):
    print(f"  {km(RT, s[:2]):4.1f} km  site {sid}  {s[2]}")

at_rt = []
overlay = defaultdict(list)
near_148500 = []
with open(D + "device_details.csv", newline="", encoding="utf-8", errors="replace") as f:
    for r in csv.DictReader(f):
        try:
            fq = int(float(r["FREQUENCY"]))
        except ValueError:
            continue
        lic = licences.get(r["LICENCE_NO"], ("", "", "", "", ""))
        who = clients.get(lic[0], "?")
        if r["SITE_ID"] in near_sites:
            at_rt.append((fq / 1e6, r["DEVICE_TYPE"], who, r.get("EIRP", "") + r.get("EIRP_UNIT", ""), r.get("EMISSION", ""), r.get("HEIGHT", ""), r["AUTHORISATION_DATE"], lic[1], lic[2], near_sites[r["SITE_ID"]][2][:30]))
        if fq in (148587500, 148962500) and r["DEVICE_TYPE"] == "T" and "GOVERNMENT TELECOM" in who.upper():
            s = sites.get(r["SITE_ID"], (0, 0, "?", "?"))
            overlay[fq].append((r["AUTHORISATION_DATE"], lic[3], s[2][:48], s[3], round(km(RT, s[:2])), r.get("EIRP", "")))
        if 148_475_000 <= fq <= 148_525_000 and r["SITE_ID"] in sites:
            s = sites[r["SITE_ID"]]
            d = km(RT, s[:2])
            if d <= 120:
                near_148500.append((d, fq / 1e6, r["DEVICE_TYPE"], s[2][:44], who[:36], r.get("EIRP", "") + r.get("EIRP_UNIT", ""), r.get("EMISSION", ""), lic[2]))

print("\n### every registration at those sites (MHz, T/R, licensee, EIRP, emission, AGL, authorised, category, status)")
for row in sorted(at_rt):
    print("  " + " | ".join(str(x) for x in row))

for fq, rows in overlay.items():
    rows.sort()
    dates = defaultdict(int)
    for row in rows:
        dates[row[0][:4]] += 1
    print(f"\n### NSW Telco Authority {fq/1e6:.4f} MHz: {len(rows)} transmitters; authorisation years: {dict(sorted(dates.items()))}")
    print("  authorised  licence issued  site                                              st  km_from_RT  eirp")
    for row in rows:
        print(f"  {row[0]:10s}  {row[1]:10s}      {row[2]:48s}  {row[3]:3s}  {row[4]:>5}  {row[5]}")

print("\n### registrations between 148.475 and 148.525 MHz within 120 km of Raymond Terrace")
for row in sorted(near_148500):
    print(f"  {row[0]:6.1f} km  {row[1]:9.4f}  {row[2]}  {row[3]:44s} {row[4]:36s} {row[5]:>8} {row[6]:9} {row[7]}")
if not near_148500:
    print("  (none)")
