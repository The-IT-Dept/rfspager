// Dumps the mbelib tables as "name rows cols" headers followed by values,
// one per line, for conversion to Go.
#include <stdio.h>
#include "ambe3600x2450_const.h"
#include "mbelib_const.h"

static void dumpf(const char *name, const float *v, int rows, int cols) {
  printf("%s %d %d\n", name, rows, cols);
  for (int i = 0; i < rows * cols; i++) printf("%.9g\n", v[i]);
}
static void dumpi(const char *name, const int *v, int rows, int cols) {
  printf("%s %d %d\n", name, rows, cols);
  for (int i = 0; i < rows * cols; i++) printf("%d\n", v[i]);
}
int main(void) {
  dumpf("ambeW0", AmbeW0table, 1, 120);
  dumpf("ambeL", AmbeLtable, 1, 120);
  dumpi("ambeVUV", &AmbeVuv[0][0], 32, 8);
  dumpi("ambeLmprbl", &AmbeLmprbl[0][0], 57, 4);
  dumpf("ambeDg", AmbeDg, 1, 32);
  dumpf("ambePRBA24", &AmbePRBA24[0][0], 512, 3);
  dumpf("ambePRBA58", &AmbePRBA58[0][0], 128, 4);
  dumpf("ambeHOCb5", &AmbeHOCb5[0][0], 32, 4);
  dumpf("ambeHOCb6", &AmbeHOCb6[0][0], 16, 4);
  dumpf("ambeHOCb7", &AmbeHOCb7[0][0], 16, 4);
  dumpf("ambeHOCb8", &AmbeHOCb8[0][0], 8, 4);
  dumpf("synthWindow", Ws, 1, 321);
  return 0;
}
