package pocsag

import "sort"

// Soft-decision repair for codewords the algebraic BCH decoder gives up on.
//
// BCH(31,21) corrects two errors. With three or four the syndrome either
// maps to nothing (reported uncorrectable) or, worse, to the wrong valid
// codeword. The slicer knows which bits it was unsure about, so a failed
// codeword is retried with the least confident bits flipped in every
// combination (Chase-2 decoding). A candidate that then verifies, parity
// included, is accepted only if the bits that had to change were all
// doubtful: flipping a confident bit means the candidate is a different
// codeword, not a repair of this one.

const (
	// chaseBits is how many of the least confident bits are tried in
	// combination: 2^chaseBits trials per failed codeword.
	chaseBits = 5
	// chaseMaxFlips bounds the total number of bits a candidate may differ
	// from the received word by, trial flips plus BCH corrections.
	chaseMaxFlips = 4
	// chaseMaxConf is the highest confidence a flipped bit may have had.
	// Confidence is the slicer level relative to the running mean, so a
	// clean symbol reads ~1.0 and a bit that landed near the threshold
	// reads near 0.
	chaseMaxConf = 0.6
)

// chase tries to repair cw using per-bit confidences (index 31 is the
// first-received bit, matching the codeword's bit numbering). It returns
// the repaired codeword and how many bits changed, or ok=false.
func chase(cw uint32, conf *[32]float64) (uint32, int, bool) {
	if conf == nil {
		return cw, -1, false
	}
	// Rank bit positions by confidence, least confident first.
	idx := make([]int, 32)
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return conf[idx[a]] < conf[idx[b]] })
	trial := idx[:chaseBits]

	bestCost := 1e9
	var best uint32
	bestFlips := -1
	for pat := 1; pat < 1<<chaseBits; pat++ {
		cand := cw
		cost := 0.0
		flips := 0
		for b := 0; b < chaseBits; b++ {
			if pat&(1<<b) != 0 {
				cand ^= 1 << uint(trial[b])
				cost += conf[trial[b]]
				flips++
			}
		}
		fixed, errs, ok := correctWithParity(cand)
		if !ok {
			continue
		}
		// Every bit BCH corrected on top of the trial flips must also have
		// been doubtful, or this is a different codeword entirely.
		diff := fixed ^ cw
		total := 0
		doubtful := true
		for i := 0; i < 32; i++ {
			if diff&(1<<uint(i)) != 0 {
				total++
				if conf[i] > chaseMaxConf {
					doubtful = false
				}
			}
		}
		_ = errs
		if !doubtful || total > chaseMaxFlips {
			continue
		}
		cost += 0.0
		for i := 0; i < 32; i++ {
			if diff&(1<<uint(i)) != 0 && (fixed^cand)&(1<<uint(i)) != 0 {
				cost += conf[i]
			}
		}
		if cost < bestCost {
			bestCost, best, bestFlips = cost, fixed, total
		}
	}
	if bestFlips < 0 {
		return cw, -1, false
	}
	return best, bestFlips, true
}
