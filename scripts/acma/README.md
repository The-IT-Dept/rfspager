# ACMA paging site lookup

Finds the licensed paging transmitters behind the frequencies the receivers
decode, from the ACMA Register of Radiocommunications Licences offline
dataset. Used on 2026-10-13 to work out why the Raymond Terrace receiver hears
FRNSW at full scale and RFS 23 dB down.

The old `web.acma.gov.au/rrl` pages closed in June 2026 and the new register is
a browser app, so query the dataset instead. It is regenerated every few hours.

```sh
mkdir -p /tmp/acma-rrl && cd /tmp/acma-rrl
curl -sL -o spectra_rrl.zip https://web.acma.gov.au/rrl-updates/spectra_rrl.zip   # ~70 MB
unzip -q spectra_rrl.zip -d data                                                   # device_details.csv is ~390 MB

python3 -I scripts/acma/paging_sites.py /tmp/acma-rrl/data      # the two paging frequencies, by distance
python3 -I scripts/acma/raymond_terrace.py /tmp/acma-rrl/data   # everything at one site, overlay dates, a spur hunt
```

Tables used: `device_details.csv` (FREQUENCY in Hz, DEVICE_TYPE T/R, SITE_ID,
LICENCE_NO, EIRP, EMISSION, HEIGHT, AUTHORISATION_DATE), `site.csv` (LATITUDE,
LONGITUDE, NAME, STATE, ELEVATION), `licence.csv` (CLIENT_NO, category, status)
and `client.csv` (LICENCEE). Edit the `TARGETS` and `REF` tables at the top of
each script for other frequencies or receiver locations.

What it showed on 2026-10-13:

- 148.9625 MHz (FRNSW) is a 34-site NSW Telco Authority paging network
  authorised 2025-11-27 at 174 W EIRP, including the Telstra exchange at
  Raymond Terrace, 300 m from that receiver.
- 148.5875 MHz (RFS) has 416 RFS-owned district transmitters statewide plus a
  36-site NSWTA overlay at the same sites, authorised 2025-11-26. The overlay
  transmitter at Raymond Terrace is not what the receiver hears; the RFS pages
  arrive from district sites 14 to 36 km away.
- Nothing is licensed near 148.500 MHz within 120 km. The strong carrier there
  turned out to be the Pi 5's HDMI pixel clock (148.5 MHz with 67.5 kHz
  line-rate sidebands), 87 kHz from the RFS channel.
