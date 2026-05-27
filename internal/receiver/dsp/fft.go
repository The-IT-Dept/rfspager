package dsp

import "math"

// FFT performs an in-place Cooley-Tukey radix-2 forward FFT on x. The length
// of x must be a power of two. Single-purpose — we don't need windowing or
// scaling baked in; callers handle those around the call.
func FFT(x []complex128) {
	n := len(x)
	if n&(n-1) != 0 || n == 0 {
		panic("dsp: FFT length must be a power of two")
	}

	// Bit-reversal permutation.
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}

	// Cooley-Tukey butterflies.
	for size := 2; size <= n; size <<= 1 {
		ang := -2 * math.Pi / float64(size)
		wstep := complex(math.Cos(ang), math.Sin(ang))
		half := size >> 1
		for i := 0; i < n; i += size {
			w := complex(1, 0)
			for j := range half {
				u := x[i+j]
				v := x[i+j+half] * w
				x[i+j] = u + v
				x[i+j+half] = u - v
				w *= wstep
			}
		}
	}
}

// HannWindow returns an N-tap Hann window. Cached by length — we only ever
// use one or two sizes so this stays tiny.
var hannCache = map[int][]float64{}

func HannWindow(n int) []float64 {
	if w, ok := hannCache[n]; ok {
		return w
	}
	w := make([]float64, n)
	for i := range n {
		w[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n-1))
	}
	hannCache[n] = w
	return w
}
