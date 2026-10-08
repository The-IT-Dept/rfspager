// Encodes S16LE 8 kHz PCM (stdin-free: path args) into P25 Phase 2 vocoder
// codewords with OP25's ambe_encoder; writes 9 packed bytes (72 bits, MSB
// first, OP25 dibit order) per 20 ms frame.
#include <stdio.h>
#include <stdint.h>
#include "imbe_vocoder/imbe_vocoder.h"
#include "mbelib.h"
#include "p25p2_vf.h"
#include "ambe_encoder.h"

int main(int argc, char **argv) {
	if (argc != 3) { fprintf(stderr, "usage: encoder in.s16 out.vcw\n"); return 2; }
	FILE *in = fopen(argv[1], "rb"), *out = fopen(argv[2], "wb");
	if (!in || !out) return 1;
	ambe_encoder enc;
	int16_t pcm[160];
	int n = 0;
	while (fread(pcm, 2, 160, in) == 160) {
		uint8_t dibits[36], packed[9] = {0};
		enc.encode(pcm, dibits);
		for (int i = 0; i < 72; i++) {
			int bit = (i & 1) ? dibits[i / 2] & 1 : (dibits[i / 2] >> 1) & 1;
			packed[i / 8] |= bit << (7 - i % 8);
		}
		fwrite(packed, 1, 9, out);
		n++;
	}
	fclose(out);
	fprintf(stderr, "%d frames\n", n);
	return 0;
}
