// Package p25 decodes APCO Project 25 Phase 1 trunking control channels
// (the NSW Public Safety Network runs Motorola P25).
//
// The pipeline mirrors the POCSAG receiver's shape:
//
//	48 kS/s complex baseband → Demod (AFC + 1-symbol differential phase)
//	  → Framer (frame sync, status-symbol strip, NID BCH(63,16))
//	  → TSBK blocks (deinterleave, ½-rate trellis Viterbi, CRC-CCITT)
//	  → typed control-channel messages (grants, affiliations, channel plans)
//
// Differential phase over one symbol period is both the TIA-102 C4FM
// "integrate and dump" receiver and the native CQPSK/LSM demodulator, so one
// demodulator serves standalone sites and simulcast cells alike.
//
// Channelising the dongle's wideband IQ down to 48 kS/s is the caller's job
// (see internal/receiver/dsp); this package only ever sees one channel.
package p25

// SampleRate is the baseband rate the demodulator expects: 10 samples per
// 4800-baud symbol.
const SampleRate = 48_000

// SymbolRate is the P25 Phase 1 symbol rate.
const SymbolRate = 4_800

const samplesPerSymbol = SampleRate / SymbolRate
