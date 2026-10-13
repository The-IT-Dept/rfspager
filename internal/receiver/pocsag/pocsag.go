// Package pocsag decodes POCSAG pages from a baseband bit stream.
//
// POCSAG ("Post Office Code Standardisation Advisory Group") is the dominant
// fire/EMS paging protocol in Australia. A transmission is:
//
//	preamble (576 bits of 1010…) | sync (0x7CD215D8) | N×batch | sync | …
//
// A batch is 8 frames; each frame is 2 codewords; each codeword is 32 bits of
// BCH(31,21)+parity. The top bit picks address (0) vs message (1). Within a
// batch, the frame index reveals the low 3 bits of an address codeword's
// capcode (the codeword carries the high 18).
//
// The frontend (FM demod → bit slicer → here) hands us a stream of *bits*
// once it has locked to the sync word. Polarity is corrected automatically by
// trying both the sync word and its inverse.
package pocsag

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// huntSyncErrors is the bit-error tolerance while hunting for the
	// first sync word. Two errors on a 32-bit word matches noise about
	// once every 8 million bits, and the second sync a batch later still
	// has to match before anything is released.
	huntSyncErrors = 2
	// verifySyncErrors applies to the confirming sync of a fresh lock.
	verifySyncErrors = 2
	// lockedSyncErrors applies once locked: the sync is checked only at
	// its expected position, so a noisier match is still safe.
	lockedSyncErrors = 3
	// The sync is looked for from two bits early to three bits late, so
	// a single slipped or repeated bit realigns instead of ending the page.
	syncWindowFirst = 30
	syncWindowLast  = 35

	SyncWord    uint32 = 0x7CD215D8 // 32-bit POCSAG batch sync
	SyncInverse uint32 = 0x832DEA27 // bitwise inverse of SyncWord

	IdleCodeword uint32 = 0x7A89C197 // "no address here" placeholder

	codewordsPerBatch = 16
)

// Page is a decoded POCSAG transmission destined for one capcode.
type Page struct {
	// Address is the 21-bit pager capcode formatted as a decimal string,
	// e.g. "0521839" — matches what the existing Pagermon ingress expects.
	Address string
	// Function is 0-3 (A/B/C/D); fire services almost always use 3.
	Function int
	// BitRate is the detected POCSAG baud (512, 1200, or 2400).
	BitRate int
	// Numeric is the BCD-decoded numeric message.
	Numeric string
	// Alpha is the 7-bit ASCII decoded message for alphanumeric pages.
	Alpha string
	// Quality records how much repair the page needed, so copies of the
	// same page can be ranked and damaged text can be flagged.
	Quality Quality
}

// Quality is the decoder's own assessment of one page.
type Quality struct {
	// Codewords is how many message codewords fed the page.
	Codewords int
	// Corrected is how many of them BCH repaired (1-2 bit errors).
	Corrected int
	// Uncorrectable is how many were beyond repair. Their raw bits were
	// kept so the characters after them stay aligned; the characters they
	// produced are listed in DamagedRanges.
	Uncorrectable int
	// Truncated is set when the page ended because sync was lost or the
	// sample stream had a gap, rather than by an idle codeword or the
	// next address.
	Truncated bool
	// DamagedRanges are [start, end) byte offsets into Alpha whose text
	// came from uncorrectable codewords.
	DamagedRanges [][2]int
}

// Decoder consumes one bit at a time (true = 1) and emits Pages on Out.
//
// Sync handling: tolerates ≤1 bit error in the 32-bit sync word (real RF
// reception easily flips a bit), then *stages* pages from that batch and
// only releases them once the next 32 bits also match the sync word. The
// joint false-sync probability across two independent matches is
// ~6e-17 — effectively zero — while still tolerating real-world noise.
type Decoder struct {
	reg      uint32
	inverted bool
	state    int // 0 = hunt, 1 = collecting batch, 2 = verifying next sync
	bitsInCW int
	cwIdx    int
	bitsSeen int // bits since starting the verify state
	batch    [codewordsPerBatch]uint32
	bitRate  int
	locked   bool // true after the FIRST sync confirmation — subsequent batches
	// can then commit on receipt instead of waiting for confirmation, and end-
	// of-transmission flushes whatever's pending instead of discarding.

	// Per-address message state — accumulated as we decode batches, only
	// released to Out via Commit() after sync confirmation.
	pendingAddr     string
	pendingFunction int
	alphaCarry      uint32 // leftover bits (low-aligned)
	alphaCarryBits  int
	alpha           strings.Builder
	numeric         strings.Builder
	pendingQ        Quality
	staged          []Page // pages waiting on next-sync confirmation

	// Health counters, read by Stats() from another goroutine. Each is one
	// atomic add on an event that happens at most once per codeword, so
	// the decode path does not notice them.
	cnt counters

	Out chan Page
}

type counters struct {
	syncLocks       atomic.Uint64
	syncLosses      atomic.Uint64
	cwClean         atomic.Uint64
	cwCorrected     atomic.Uint64
	cwUncorrectable atomic.Uint64
	pages           atomic.Uint64
	pagesTruncated  atomic.Uint64
	lastPageNanos   atomic.Int64
}

// Stats is a cumulative snapshot of a decoder's counters since it was
// created. Callers that want per-interval numbers keep the previous
// snapshot and Sub it.
type Stats struct {
	SyncLocks       uint64 // second sync word seen after a hunt: a confirmed lock
	SyncLosses      uint64 // a confirmed lock ended (sync missing, or a stream gap)
	CWClean         uint64 // codewords that BCH-verified with no errors
	CWCorrected     uint64 // codewords repaired (1-2 bit errors)
	CWUncorrectable uint64 // codewords beyond BCH's reach, dropped
	Pages           uint64 // pages released to Out
	PagesTruncated  uint64 // pages cut short by a stream gap or a lost sync
	// LastPageAt is when the most recent page was released; zero if none.
	LastPageAt time.Time
}

// Stats returns the counters so far. Safe to call from any goroutine.
func (d *Decoder) Stats() Stats {
	s := Stats{
		SyncLocks:       d.cnt.syncLocks.Load(),
		SyncLosses:      d.cnt.syncLosses.Load(),
		CWClean:         d.cnt.cwClean.Load(),
		CWCorrected:     d.cnt.cwCorrected.Load(),
		CWUncorrectable: d.cnt.cwUncorrectable.Load(),
		Pages:           d.cnt.pages.Load(),
		PagesTruncated:  d.cnt.pagesTruncated.Load(),
	}
	if ns := d.cnt.lastPageNanos.Load(); ns != 0 {
		s.LastPageAt = time.Unix(0, ns)
	}
	return s
}

// Add sums two snapshots; a channel runs one decoder per baud and reports
// them as one.
func (s Stats) Add(o Stats) Stats {
	s.SyncLocks += o.SyncLocks
	s.SyncLosses += o.SyncLosses
	s.CWClean += o.CWClean
	s.CWCorrected += o.CWCorrected
	s.CWUncorrectable += o.CWUncorrectable
	s.Pages += o.Pages
	s.PagesTruncated += o.PagesTruncated
	if o.LastPageAt.After(s.LastPageAt) {
		s.LastPageAt = o.LastPageAt
	}
	return s
}

// Sub returns the interval delta s - prev for the counters; LastPageAt is
// carried over from s.
func (s Stats) Sub(prev Stats) Stats {
	return Stats{
		SyncLocks:       s.SyncLocks - prev.SyncLocks,
		SyncLosses:      s.SyncLosses - prev.SyncLosses,
		CWClean:         s.CWClean - prev.CWClean,
		CWCorrected:     s.CWCorrected - prev.CWCorrected,
		CWUncorrectable: s.CWUncorrectable - prev.CWUncorrectable,
		Pages:           s.Pages - prev.Pages,
		PagesTruncated:  s.PagesTruncated - prev.PagesTruncated,
		LastPageAt:      s.LastPageAt,
	}
}

func New(bitRate int, out chan Page) *Decoder {
	return &Decoder{bitRate: bitRate, Out: out}
}

// Feed consumes one received bit.
func (d *Decoder) Feed(b bool) {
	d.reg = (d.reg << 1)
	if b {
		d.reg |= 1
	}
	switch d.state {
	case 0: // hunting for sync — tolerate up to 2 bit errors; the second
		// sync a batch later still has to match before anything is released.
		if hammingDist32(d.reg, SyncWord) <= huntSyncErrors {
			d.state, d.inverted, d.bitsInCW, d.cwIdx = 1, false, 0, 0
			d.locked = false
		} else if hammingDist32(d.reg, SyncInverse) <= huntSyncErrors {
			d.state, d.inverted, d.bitsInCW, d.cwIdx = 1, true, 0, 0
			d.locked = false
		}
	case 1: // collecting 16 codewords into batch
		d.bitsInCW++
		if d.bitsInCW == 32 {
			cw := d.reg
			if d.inverted {
				cw = ^cw
			}
			d.batch[d.cwIdx] = cw
			d.cwIdx++
			d.bitsInCW = 0
			if d.cwIdx == codewordsPerBatch {
				// Stage the batch's pages, then wait for the next sync to
				// confirm before releasing them.
				d.processBatch()
				d.state = 2
				d.bitsSeen = 0
			}
		}
	case 2: // verifying that the next sync word arrives (about) on time
		d.bitsSeen++
		if d.bitsSeen >= syncWindowFirst {
			// Weak signals slip a bit now and then: the sync may land a
			// couple of bits early or late. Look for it across a small
			// window around the expected position and realign on it, and
			// once locked accept a noisier match than during the hunt.
			maxErr := verifySyncErrors
			if d.locked {
				maxErr = lockedSyncErrors
			}
			matchNorm := hammingDist32(d.reg, SyncWord) <= maxErr
			matchInv := hammingDist32(d.reg, SyncInverse) <= maxErr
			if !matchNorm && !matchInv && d.bitsSeen < syncWindowLast {
				return // keep looking a little longer
			}
			if matchNorm || matchInv {
				if !d.locked {
					d.cnt.syncLocks.Add(1)
				}
				d.locked = true
				d.commitStaged()
				// Continue locked: another batch follows.
				d.inverted = matchInv
				d.state, d.bitsInCW, d.cwIdx = 1, 0, 0
			} else if d.locked {
				// End of transmission after a confirmed lock — flush
				// whatever message was still being accumulated and emit.
				d.cnt.syncLosses.Add(1)
				d.markTruncated()
				d.flushPending()
				d.commitStaged()
				d.state = 0
				d.locked = false
			} else {
				// First sync but no second sync → false lock. Discard.
				d.discardStaged()
				d.state = 0
			}
		}
	}
}

// Resync tells the decoder the incoming bit stream has a discontinuity —
// the SDR layer dropped IQ buffers, so an unknown number of bits are simply
// missing and every codeword boundary after this point is misaligned until
// the next sync word. Whatever message was in flight is finalised with the
// codewords received so far (all of which BCH-verified, so the prefix is
// trustworthy) and released, then the decoder goes back to hunting for
// sync. Without this, the misaligned remainder of the batch is fed through
// BCH, and roughly half of those garbage codewords "correct" into valid-
// looking ones: real pages grow junk tails and phantom pages appear on
// random capcodes.
func (d *Decoder) Resync() {
	// Codewords are buffered until the batch completes; process the ones we
	// already have so the message prefix they carry is not thrown away.
	if d.state == 1 && d.cwIdx > 0 {
		d.processCodewords(d.batch[:d.cwIdx])
	}
	if d.locked {
		d.cnt.syncLosses.Add(1)
	}
	d.markTruncated()
	d.flushPending()
	d.commitStaged()
	d.state = 0
	d.locked = false
	d.bitsInCW, d.cwIdx, d.bitsSeen = 0, 0, 0
	d.reg = 0
}

func (d *Decoder) processBatch() { d.processCodewords(d.batch[:]) }

// processCodewords decodes a run of codewords starting at frame 0 of a batch.
// It may be handed a partial batch on Resync.
func (d *Decoder) processCodewords(cws []uint32) {
	for i, cw := range cws {
		// Catch noisy idle codewords before BCH gets a chance to "correct"
		// them into a phantom address. The idle pattern (0x7A89C197) is far
		// from any plausible address/message codeword in Hamming space, so
		// anything within 3 bits of idle is overwhelmingly likely to have
		// started life as idle padding at the end of a real message.
		// Without this guard, a 3-error idle codeword can BCH-correct into
		// a "valid" address and the next noisy codeword gets appended as
		// its message text — producing the OUR^j|UbL / 52r#H / :Y*X noise
		// companions we see at end-of-burst.
		if n := popcount32(cw ^ IdleCodeword); n <= 3 {
			if n == 0 {
				d.cnt.cwClean.Add(1)
			} else {
				d.cnt.cwCorrected.Add(1)
			}
			d.flushPending()
			continue
		}
		corrected, errors, ok := correctWithParity(cw)
		switch {
		case !ok:
			d.cnt.cwUncorrectable.Add(1)
		case errors == 0:
			d.cnt.cwClean.Add(1)
		default:
			d.cnt.cwCorrected.Add(1)
		}
		if !ok {
			// Beyond BCH's reach. Alphanumeric characters are 7 bits packed
			// across 20-bit codewords, so dropping the codeword would shift
			// every character after it by six bits and turn the rest of the
			// page into garbage. Keep its raw bits instead: at most four
			// characters are wrong, the page stays aligned, and the damaged
			// span is reported on the page. With no page in flight the word
			// may have been an address we can no longer trust, so skip it.
			if d.pendingAddr != "" {
				d.pendingQ.Codewords++
				d.pendingQ.Uncorrectable++
				d.appendMessageBits(cw|0x80000000, true)
			}
			continue
		}
		if corrected&0x80000000 != 0 {
			if d.pendingAddr != "" {
				d.pendingQ.Codewords++
				if errors > 0 {
					d.pendingQ.Corrected++
				}
			}
			d.appendMessageBits(corrected, false)
			continue
		}
		// Address codeword arriving mid-message: protect long real messages
		// from BCH false-positives.
		//
		//   • short in-flight (<100 chars of plausible alpha): require a
		//     clean (0-error) BCH correction. A noisy *message* codeword
		//     can syndrome-map to a "valid" address pattern about half the
		//     time, and we'd rather lose a genuine multi-capcode early than
		//     truncate every long burst.
		//
		//   • long in-flight (≥100 chars): the message is overwhelmingly
		//     likely to be real and continuing, so tolerate up to 1 BCH
		//     error on the candidate address before yanking. This recovers
		//     the tail-end truncations we saw in real RFS dispatches.
		//
		//   • either threshold: 2-error corrections are too lossy to trust
		//     as an address mid-stream.
		//
		// REVERT NOTE: if this starts merging unrelated capcodes' messages
		// together, change the `> 1` back to `> 0` and remove the threshold.
		if d.pendingAddr != "" {
			alphaLen := d.alpha.Len()
			maxErrors := 0
			if alphaLen >= 100 {
				maxErrors = 1
			}
			if errors > maxErrors {
				continue
			}
		}
		// Complete the previous page, then capture new addr.
		d.flushPending()
		if corrected == IdleCodeword {
			continue
		}
		frameIdx := i / 2
		addrHigh := (corrected >> 13) & 0x3FFFF
		addrFull := (addrHigh << 3) | uint32(frameIdx)
		d.pendingAddr = fmt.Sprintf("%07d", addrFull)
		d.pendingFunction = int((corrected >> 11) & 0x3)
	}
	// NOTE: do NOT flushPending at end-of-batch. POCSAG messages routinely
	// span multiple batches; the message codewords following the address
	// just keep streaming until an idle codeword, a new address, or end of
	// transmission. End-of-transmission flush happens in Feed() when state 2
	// detects no further sync word.
}

// appendMessageBits consumes one message codeword's 20-bit payload.
//
// POCSAG message bits arrive MSB-first within the codeword (bit 30..bit 11).
// Numeric pages pack 5 BCD digits per codeword, each digit LSB-first.
// Alpha pages stream 7-bit ASCII characters LSB-first, with characters
// running across codeword boundaries.
func (d *Decoder) appendMessageBits(cw uint32, damaged bool) {
	if d.pendingAddr == "" {
		return
	}
	data := (cw >> 11) & 0xFFFFF // 20 bits in bits 19..0
	alphaBefore := d.alpha.Len()
	defer func() {
		if damaged && d.alpha.Len() > alphaBefore {
			d.pendingQ.DamagedRanges = append(d.pendingQ.DamagedRanges, [2]int{alphaBefore, d.alpha.Len()})
		}
	}()

	// Numeric: 5 digits, each 4 bits, in transmission order.
	for i := range 5 {
		shift := uint(16 - i*4) // i=0 → bits 19..16 = first-arrived nibble
		nyb := reverse4(uint8((data >> shift) & 0xF))
		d.numeric.WriteByte(numericChar(nyb))
	}

	// Alpha: append 20 bits to the carry, pop 7-bit chars.
	bits := uint64(d.alphaCarry)<<20 | uint64(data)
	total := d.alphaCarryBits + 20
	for total >= 7 {
		shift := uint(total - 7)
		chunk := uint8((bits >> shift) & 0x7F)
		mask := uint64(1)<<shift - 1
		bits &= mask
		total -= 7
		c := reverse7(chunk)
		if c >= 0x20 && c < 0x7F {
			d.alpha.WriteByte(c)
		} else if c == 0x0A || c == 0x0D {
			d.alpha.WriteByte(c)
		}
	}
	d.alphaCarry = uint32(bits)
	d.alphaCarryBits = total
}

func (d *Decoder) flushPending() {
	if d.pendingAddr == "" {
		return
	}
	page := Page{
		Address:  d.pendingAddr,
		Function: d.pendingFunction,
		BitRate:  d.bitRate,
		Numeric:  strings.TrimRight(d.numeric.String(), " "),
		Alpha:    d.alpha.String(),
		Quality:  d.pendingQ,
	}
	d.pendingAddr = ""
	d.pendingFunction = 0
	d.numeric.Reset()
	d.alpha.Reset()
	d.alphaCarry = 0
	d.alphaCarryBits = 0
	d.pendingQ = Quality{}
	// A page whose every message codeword was beyond repair is noise
	// that followed a mis-decoded address, not a page.
	if page.Quality.Codewords > 0 && page.Quality.Uncorrectable == page.Quality.Codewords {
		return
	}
	if page.Alpha != "" || page.Numeric != "" {
		d.staged = append(d.staged, page)
	}
}

// commitStaged releases pages from the most recent batch to Out, having
// confirmed lock by seeing a second valid sync word.
func (d *Decoder) commitStaged() {
	if d.Out == nil {
		d.staged = nil
		return
	}
	for _, p := range d.staged {
		d.Out <- p
		d.cnt.pages.Add(1)
		d.cnt.lastPageNanos.Store(time.Now().UnixNano())
	}
	d.staged = nil
}

// noteTruncation records that the page in flight (an address with at least
// one message codeword behind it) is about to be finalised early because
// the stream ended or had a gap, rather than because the transmitter sent
// an idle codeword or the next address.
func (d *Decoder) noteTruncation() {
	if d.pendingAddr != "" && (d.alpha.Len() > 0 || d.numeric.Len() > 0) {
		d.cnt.pagesTruncated.Add(1)
	}
}

// markTruncated is noteTruncation plus the flag on the page itself.
func (d *Decoder) markTruncated() {
	d.noteTruncation()
	if d.pendingAddr != "" && (d.alpha.Len() > 0 || d.numeric.Len() > 0) {
		d.pendingQ.Truncated = true
	}
}

// discardStaged drops the most recent batch's staged pages — sync didn't
// reappear so the lock was probably bogus.
func (d *Decoder) discardStaged() {
	d.staged = nil
	d.pendingAddr = ""
	d.pendingFunction = 0
	d.numeric.Reset()
	d.alpha.Reset()
	d.alphaCarry = 0
	d.alphaCarryBits = 0
	d.pendingQ = Quality{}
}

func reverse4(v uint8) uint8 {
	v = (v&0x1)<<3 | (v&0x2)<<1 | (v&0x4)>>1 | (v&0x8)>>3
	return v & 0xF
}

func reverse7(v uint8) byte {
	v &= 0x7F
	var r uint8
	for i := range 7 {
		if v&(1<<i) != 0 {
			r |= 1 << (6 - i)
		}
	}
	return r
}

func numericChar(n uint8) byte {
	const table = "0123456789*U -)("
	return table[n&0xF]
}

// hammingDist32 counts differing bits between a and b.
func hammingDist32(a, b uint32) int {
	x := a ^ b
	c := 0
	for x != 0 {
		c++
		x &= x - 1
	}
	return c
}

// FormatPage renders a Page for human-readable logging.
func FormatPage(p Page) string {
	msg := p.Alpha
	if msg == "" {
		msg = p.Numeric
	}
	fn := 'A' + rune(p.Function)
	return fmt.Sprintf("[%4d] %s/%c %s", p.BitRate, p.Address, fn, strings.TrimSpace(msg))
}
