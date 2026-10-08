// Thin C ABI over OP25's p25p2_vf so the C harness can use its Phase 2
// vocoder-frame extraction, Golay decode, PRNG descrambling and encoder.
#include <stdint.h>
#include <stddef.h>
#define private public
#include "p25p2_vf.h"
#undef private

extern "C" {
size_t op25_process(const uint8_t dibits[36], int u[4], int c[4]) {
	p25p2_vf vf;
	mbe_errs e;
	e.E0 = e.E1 = 0;
	e.ER = 0;
	int b[9];
	vf.extract_vcw(dibits, c[0], c[1], c[2], c[3]);
	return vf.process_vcw(&e, dibits, b, u);
}

void op25_encode(const int b[9], uint8_t dibits[36]) {
	p25p2_vf vf;
	vf.encode_vcw(dibits, b);
}

void op25_unpack_b(const int u[4], int b[9]) {
	p25p2_vf vf;
	vf.unpack_b(b, u);
}
}
