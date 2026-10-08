#!/bin/sh
# Regenerates ../mbelib_f32.bin.gz, ../mbelib_f64.bin.gz and
# ../op25enc_tones.vcw from mbelib and OP25 source checkouts.
#
#   build.sh <mbelib-dir> <op25-repo-dir> <scratch-dir>
#
# mbelib: github.com/szechyjs/mbelib; OP25: github.com/boatbod/op25.
# Everything is built in <scratch-dir>; only the three outputs are copied
# back. Needs cc, c++ and gzip.
set -eu
MB=$(cd "$1" && pwd)
OP=$(cd "$2" && pwd)/op25/gr-op25_repeater/lib
OUT=$(cd "$(dirname "$0")/.." && pwd)
HERE=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$3"
cd "$3"
CC="cc -O2 -w -ffp-contract=off"
CXX="c++ -std=c++11 -O2 -w"

# OP25 vocoder-frame code and AMBE encoder.
$CXX -I"$OP" -c "$HERE/op25_side.cc" -o op25_side.o
$CXX -I"$OP" -c "$OP/p25p2_vf.cc" -o p25p2_vf.o
$CXX -I"$OP" -c "$OP/rs.cc" -o rs.o
mkdir -p enc
for f in "$OP"/imbe_vocoder/*.cc; do
	$CXX -I"$OP" -I"$OP/imbe_vocoder" -c "$f" -o "enc/$(basename "$f" .cc).o"
done
$CXX -I"$OP" -c "$OP/ambe_encoder.cc" -o enc/ambe_encoder.o
$CC -I"$OP" -c "$OP/ambe.c" -o enc/ambe.o
$CC -I"$OP" -c "$OP/mbelib.c" -o enc/mbelib.o
$CXX -I"$OP" -c "$HERE/encoder_main.cc" -o enc/encoder_main.o
c++ -o encoder enc/*.o p25p2_vf.o rs.o -lm

# Test signal → OP25 AMBE encoder → codewords.
cc -O2 "$HERE/mkaudio.c" -o mkaudio -lm
./mkaudio tones.s16
./encoder tones.s16 tones.vcw

# mbelib, stock (float) and with float arithmetic promoted to double. The
# double build keeps the tables float32 by including the const headers
# before the #define; rand() is replaced by the harness's ref_rand().
mkdir -p f32 f64
for s in ambe3600x2450 mbelib ecc; do
	$CC -Drand=ref_rand -I"$MB" -c "$MB/$s.c" -o f32/$s.o
	{
		echo '#include <stdlib.h>'
		echo '#include <stdio.h>'
		echo '#include <math.h>'
		case $s in
		ambe3600x2450) echo '#include "ambe3600x2450_const.h"' ;;
		mbelib) echo '#include "mbelib_const.h"' ;;
		esac
		echo '#define float double'
		echo '#define cosf cos'
		echo '#define sqrtf sqrt'
		echo '#define powf pow'
		echo "#include \"$MB/$s.c\""
	} > f64/wrap_$s.c
	$CC -Drand=ref_rand -I"$MB" -c f64/wrap_$s.c -o f64/$s.o
done
$CC -I"$MB" -c "$HERE/harness.c" -o f32/harness.o
{
	echo '#include <stdlib.h>'
	echo '#include <stdio.h>'
	echo '#include <stdint.h>'
	echo '#include <string.h>'
	echo '#include <math.h>'
	echo '// Records keep float32 fields even when mbelib is built in double.'
	echo 'static void put_f32(FILE *f, double v) { float x = (float)v; fwrite(&x, 4, 1, f); }'
	echo '#define HAVE_PUT_F32'
	echo '#define float double'
	echo "#include \"$HERE/harness.c\""
} > f64/wrap_harness.c
$CC -I"$MB" -c f64/wrap_harness.c -o f64/harness.o
for v in f32 f64; do
	c++ -o harness_$v $v/*.o op25_side.o p25p2_vf.o rs.o -lm
	./harness_$v tones.vcw mbelib_$v.bin
	gzip -9nc mbelib_$v.bin > "$OUT/mbelib_$v.bin.gz"
done
cp tones.vcw "$OUT/op25enc_tones.vcw"
