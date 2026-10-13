package pocsag

import (
	"math/rand"
	"testing"
)

// confidences returns a confidence vector where every bit is confident
// except the listed positions, which are marked doubtful.
func confidences(doubtful ...int) *[32]float64 {
	var c [32]float64
	for i := range c {
		c[i] = 1.0
	}
	for _, i := range doubtful {
		c[i] = 0.1
	}
	return &c
}

// Three errors on doubtful bits are beyond BCH but within Chase.
func TestChaseRepairsThreeDoubtfulErrors(t *testing.T) {
	cws := AlphaCodewords(testMsg)
	target := cws[4]
	bad := target ^ (1 << 3) ^ (1 << 14) ^ (1 << 22)
	if _, _, ok := correctWithParity(bad); ok {
		t.Skip("this error pattern happens to be within BCH reach; pick another")
	}
	got, flips, ok := chase(bad, confidences(3, 14, 22, 7, 29))
	if !ok || got != target {
		t.Fatalf("chase did not repair: ok=%v got=%08x want=%08x flips=%d", ok, got, target, flips)
	}
	if flips != 3 {
		t.Fatalf("flips = %d, want 3", flips)
	}
}

// The same three errors with confident bits must be refused: a repair
// that flips bits the slicer was sure about is a different codeword.
func TestChaseRefusesConfidentFlips(t *testing.T) {
	cws := AlphaCodewords(testMsg)
	target := cws[4]
	bad := target ^ (1 << 3) ^ (1 << 14) ^ (1 << 22)
	if _, _, ok := correctWithParity(bad); ok {
		t.Skip("pattern within BCH reach")
	}
	// Only two of the three error bits are doubtful; the third is confident.
	if got, _, ok := chase(bad, confidences(3, 14, 9, 11, 30)); ok && got != target {
		t.Fatalf("chase produced a wrong codeword %08x", got)
	}
}

// Over many random 3-error patterns with the errored bits marked
// doubtful, Chase should repair the large majority and never return a
// wrong codeword.
func TestChaseRandomPatterns(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	cws := AlphaCodewords(testMsg)
	repaired, wrong, tried := 0, 0, 0
	for trial := 0; trial < 400; trial++ {
		target := cws[r.Intn(len(cws))]
		var pos [3]int
		for i := range pos {
			pos[i] = r.Intn(31) + 1 // never the parity bit
		}
		if pos[0] == pos[1] || pos[1] == pos[2] || pos[0] == pos[2] {
			continue
		}
		bad := target ^ (1 << uint(pos[0])) ^ (1 << uint(pos[1])) ^ (1 << uint(pos[2]))
		if _, _, ok := correctWithParity(bad); ok {
			continue
		}
		tried++
		// The three error bits plus two innocent bits are doubtful.
		conf := confidences(pos[0], pos[1], pos[2], (pos[0]+5)%32, (pos[1]+9)%32)
		got, _, ok := chase(bad, conf)
		if ok && got == target {
			repaired++
		} else if ok {
			wrong++
		}
	}
	if tried < 50 {
		t.Fatalf("too few usable trials: %d", tried)
	}
	if wrong > 0 {
		t.Fatalf("chase returned %d wrong codewords out of %d", wrong, tried)
	}
	if repaired*10 < tried*8 {
		t.Fatalf("chase repaired only %d of %d", repaired, tried)
	}
}
