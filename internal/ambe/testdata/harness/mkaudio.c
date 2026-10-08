// Test signal: silence, 400 Hz sine, silence, 150 Hz harmonic buzz.
#include <stdio.h>
#include <stdint.h>
#include <math.h>
int main(int argc, char **argv) {
	FILE *f = fopen(argv[1], "wb");
	int segs[4] = {20, 50, 15, 50};
	long n = 0;
	for (int s = 0; s < 4; s++)
		for (int i = 0; i < segs[s] * 160; i++, n++) {
			double t = n / 8000.0, v = 0;
			if (s == 1) v = 8000 * sin(2 * M_PI * 400 * t);
			if (s == 3) for (int h = 1; h <= 20; h++) v += 3000.0 / h * sin(2 * M_PI * 150 * h * t);
			int16_t x = (int16_t)lrint(v);
			fwrite(&x, 2, 1, f);
		}
	fclose(f);
	return 0;
}
