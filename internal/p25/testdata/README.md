# P25 control channel fixtures

Real NSW PSN control channel captures, recorded 2026-10-08 ~11:40 AEDT from
the Central Coast on an RTL-SDR (R828D, serial 00000001) through the
Budgewoi pager antenna splitter, then channelised offline:

- format: interleaved signed 8-bit I/Q (`cs8`), 48 000 samples/s, channel at
  0 Hz (residual tuning error ~ −0.5 to −1.7 kHz; the demodulator's AFC removes it)
- channel filter: 6.25 kHz lowpass, two-stage decimation from 2.4 MS/s

| file | seconds | control channel | site | NAC |
|---|---|---|---|---|
| `cc_420.2983.cs8` | 30 | 420.3000 MHz (5-48) | RFSS 4 site 53 | 2D2 |
| `cc_419.1125.cs8` | 15 | 419.1125 MHz | RFSS 4 site 62 (strong neighbour 12.5 kHz up) | 2D2 |
| `cc_419.0250.cs8` | 15 | 419.0250 MHz (4-1048) | RFSS 4 site 51 | 2DF |

The first and last decode with 100% TSBK CRC; 419.1125 is deliberately the
hard one (~70%). `cc_419.0250.cs8` contains one unit registration and one
affiliation (unit 2039407 → talkgroup 10047, RFS Central Coast A).
