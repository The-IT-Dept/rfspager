// Golden-vector generator for internal/ambe. Builds P25 Phase 2 vocoder
// codewords with OP25's encoder, optionally corrupts them, decodes them with
// OP25's extraction + mbelib's ECC/dequantisation/synthesis and writes one
// record per frame. Built twice by build.sh: stock (float) and with float
// arithmetic promoted to double.
#include <stdio.h>
#include <stdlib.h>
#include <stdint.h>
#include <string.h>
#include "mbelib.h"

size_t op25_process(const uint8_t dibits[36], int u[4], int c[4]);
void op25_encode(const int b[9], uint8_t dibits[36]);
void op25_unpack_b(const int u[4], int b[9]);

// Deterministic replacement for libc rand() (mbelib.c is compiled with
// -Drand=ref_rand). Must match ambe.Decoder's noise generator.
static uint32_t ref_state;
int ref_rand(void) {
	ref_state ^= ref_state << 13;
	ref_state ^= ref_state >> 17;
	ref_state ^= ref_state << 5;
	return (int)(ref_state >> 1);
}

static uint64_t gen_state = 0x0123456789abcdefULL;
static uint32_t gen(void) {
	gen_state ^= gen_state << 13;
	gen_state ^= gen_state >> 7;
	gen_state ^= gen_state << 17;
	return (uint32_t)(gen_state >> 11);
}
static int rnd(int n) { return (int)(gen() % (uint32_t)n); }

static uint32_t golay23_encode(uint32_t d) {
	uint32_t r = d << 11;
	for (int i = 22; i >= 11; i--)
		if (r & (1u << i)) r ^= 0xC75u << (i - 11);
	return (d << 11) | r;
}

static mbe_parms cur, prev, prev_enh;
static FILE *out;

static void w8(int v) { uint8_t x = (uint8_t)v; fwrite(&x, 1, 1, out); }
static void w16(int v) { uint16_t x = (uint16_t)v; fwrite(&x, 2, 1, out); }
#ifndef HAVE_PUT_F32
static void put_f32(FILE *f, double v) { float x = (float)v; fwrite(&x, 4, 1, f); }
#endif
static void wf(double v) { put_f32(out, v); }

static void write_state(const short *pcm) {
	wf(prev.w0);
	w8(prev.L);
	wf(prev.gamma);
	w8(prev.repeat);
	for (int l = 1; l <= 56; l++) w8(prev.Vl[l]);
	for (int l = 1; l <= 56; l++) wf(prev.Ml[l]);
	fwrite(pcm, 2, 160, out);
}

static int mismatches;

static void decode_frame(int seq, const uint8_t bits[72]) {
	uint8_t dibits[36];
	for (int i = 0; i < 36; i++) dibits[i] = (bits[2 * i] << 1) | bits[2 * i + 1];
	int u[4], c[4];
	// OP25's own count is not used: its gly23127DecTbl gets the data bits
	// right but not the check bits, so popcount(correction) over-counts.
	// Count the true distance to the decoded codewords instead.
	op25_process(dibits, u, c);
	uint32_t cw0 = golay23_encode((uint32_t)u[0]);
	int e0 = __builtin_popcount(cw0 ^ ((uint32_t)c[0] >> 1));
	uint32_t pr = 16 * (uint32_t)u[0], m1 = 0;
	for (int i = 0; i < 23; i++) {
		pr = (173 * pr + 13849) & 0xffff;
		m1 = (m1 << 1) | (pr >> 15);
	}
	int e1 = __builtin_popcount(golay23_encode((uint32_t)u[1]) ^ ((uint32_t)c[1] ^ m1));
	int parity = (__builtin_popcount(cw0) + (c[0] & 1)) & 1;
	e0 += parity;
	int uncorrectable = e0 >= 4;
	int errs = uncorrectable ? 99 : e0 + e1;

	char ambe_fr[4][24], ambe_d[49], err_str[256];
	static const int widths[4] = {24, 23, 11, 14};
	for (int k = 0; k < 4; k++)
		for (int j = 0; j < widths[k]; j++) ambe_fr[k][j] = (c[k] >> j) & 1;
	int merrs = mbe_eccAmbe3600x2450C0(ambe_fr);
	mbe_demodulateAmbe3600x2450Data(ambe_fr);
	int merrs2 = merrs + mbe_eccAmbe3600x2450Data(ambe_fr, ambe_d);
	// mbelib's ambe_d must equal OP25's u words, MSB first.
	int p = 0;
	static const int uw[4] = {12, 12, 11, 14};
	for (int k = 0; k < 4; k++)
		for (int j = uw[k] - 1; j >= 0; j--)
			if (ambe_d[p++] != ((u[k] >> j) & 1)) mismatches++;
	if (uncorrectable) merrs2 = 99;

	float fbuf[160];
	short pcm[160];
	mbe_processAmbe2450Dataf(fbuf, &merrs, &merrs2, err_str, ambe_d, &cur, &prev, &prev_enh, 3);
	mbe_floattoshort(fbuf, pcm);

	w8(seq);
	w8(0);
	uint8_t packed[9] = {0};
	for (int i = 0; i < 72; i++) packed[i / 8] |= bits[i] << (7 - i % 8);
	fwrite(packed, 1, 9, out);
	for (int k = 0; k < 4; k++) w16(u[k]);
	w16(errs);
	write_state(pcm);
}

// erasure mirrors the repeat branch of mbe_processAmbe2450Dataf without new
// bits: what ambe.Decoder.Erasure does.
static void erasure(int seq) {
	float fbuf[160];
	short pcm[160];
	cur.repeat = prev.repeat;
	mbe_useLastMbeParms(&cur, &prev);
	cur.repeat++;
	if (cur.repeat <= 3) {
		mbe_moveMbeParms(&cur, &prev);
		mbe_spectralAmpEnhance(&cur);
		mbe_synthesizeSpeechf(fbuf, &cur, &prev_enh, 3);
		mbe_moveMbeParms(&cur, &prev_enh);
	} else {
		mbe_synthesizeSilencef(fbuf);
		mbe_initMbeParms(&cur, &prev, &prev_enh);
	}
	mbe_floattoshort(fbuf, pcm);
	w8(seq);
	w8(1);
	uint8_t zero[9] = {0};
	fwrite(zero, 1, 9, out);
	for (int k = 0; k < 4; k++) w16(0);
	w16(0);
	write_state(pcm);
}

static void start(void) {
	mbe_initMbeParms(&cur, &prev, &prev_enh);
	ref_state = 0x9E3779B9u;
}

static void encode_bits(const int b[9], uint8_t bits[72]) {
	uint8_t dibits[36];
	op25_encode(b, dibits);
	for (int i = 0; i < 36; i++) {
		bits[2 * i] = (dibits[i] >> 1) & 1;
		bits[2 * i + 1] = dibits[i] & 1;
	}
}

static int clamp(int v, int lo, int hi) { return v < lo ? lo : v > hi ? hi : v; }

// speech_b fills b with a slowly varying, speech-like parameter set.
static void speech_b(int b[9], int *b0, int *b1, int *b2) {
	*b0 = clamp(*b0 + rnd(7) - 3, 0, 119);
	if (rnd(5) == 0) *b1 = rnd(4) == 0 ? rnd(32) : rnd(16);
	*b2 = clamp(*b2 + rnd(5) - 2, 6, 30);
	b[0] = *b0;
	b[1] = *b1;
	b[2] = *b2;
	b[3] = rnd(512);
	b[4] = rnd(128);
	b[5] = rnd(32);
	b[6] = rnd(16);
	b[7] = rnd(16);
	b[8] = rnd(8);
}

int main(int argc, char **argv) {
	if (argc != 3) {
		fprintf(stderr, "usage: harness encoded.vcw out.bin\n");
		return 2;
	}
	out = fopen(argv[2], "wb");
	uint8_t bits[72];
	int b[9];

	// Sequence 0: clean speech-like frames, with a few silence frames.
	start();
	int b0 = 40, b1 = 0, b2 = 16;
	for (int f = 0; f < 100; f++) {
		speech_b(b, &b0, &b1, &b2);
		if (f == 50 || f == 51) b[0] = 124 + (f & 1);
		encode_bits(b, bits);
		decode_frame(0, bits);
	}

	// Sequence 1: clean uniformly random parameters (covers erasure, tone
	// and silence b0 values and wild jumps in L).
	start();
	static const int bw[9] = {7, 5, 5, 9, 7, 5, 4, 4, 3};
	for (int f = 0; f < 80; f++) {
		for (int k = 0; k < 9; k++) b[k] = rnd(1 << bw[k]);
		encode_bits(b, bits);
		decode_frame(1, bits);
	}

	// Sequence 2: speech-like frames through a noisy channel, with missing
	// bursts and a long bad run that must end in muting.
	start();
	b0 = 70, b1 = 2, b2 = 20;
	for (int f = 0; f < 120; f++) {
		if (f == 30 || f == 31 || (f >= 90 && f <= 95)) {
			erasure(2);
			continue;
		}
		speech_b(b, &b0, &b1, &b2);
		encode_bits(b, bits);
		int r = rnd(100), k;
		if (f >= 60 && f <= 66) k = 9;
		else if (r < 55) k = 0;
		else if (r < 70) k = 1 + rnd(2);
		else if (r < 80) k = 3;
		else if (r < 92) k = 4 + rnd(2);
		else k = 6 + rnd(4);
		for (int i = 0; i < k; i++) bits[rnd(72)] ^= 1;
		decode_frame(2, bits);
	}
	// Sequence 3: real encoder output (OP25 ambe_encoder on a test signal).
	start();
	FILE *enc = fopen(argv[1], "rb");
	uint8_t packed[9];
	while (fread(packed, 1, 9, enc) == 9) {
		for (int i = 0; i < 72; i++) bits[i] = (packed[i / 8] >> (7 - i % 8)) & 1;
		decode_frame(3, bits);
	}
	fclose(enc);
	fclose(out);
	fprintf(stderr, "ambe_d/u mismatches: %d\n", mismatches);
	return mismatches != 0;
}
