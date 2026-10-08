package ambe

// Decoder holds the inter-frame state AMBE+2 needs (previous-frame
// parameters for differential decoding, synthesis phase/overlap state).
// One per voice stream (per TDMA slot / call). Not safe for concurrent use.
type Decoder struct {
	// cur is scratch for the frame being decoded; prev is the last frame's
	// parameters as decoded (the differential reference); prevEnh is the
	// last frame as synthesised (after enhancement), which the overlap-add
	// fades out. Same three-way split as mbelib's cur/prev/prev_enhanced.
	cur, prev, prevEnh mbeParams
	rng                noise
	buf                [frameSamples]float64
}

// maxRepeats is how many consecutive frames are repeated before the decoder
// gives up and mutes (mbelib: repeat > 3 mutes).
const maxRepeats = 3

// badFrameErrs is mbelib's threshold: more than this many data bits
// corrected in c0+c1 and the frame is not trusted.
const badFrameErrs = 3

// NewDecoder returns a decoder in the start-of-call state.
func NewDecoder() *Decoder {
	d := &Decoder{}
	d.Reset()
	return d
}

// Reset returns the decoder to its start-of-call state.
func (d *Decoder) Reset() {
	d.prev.reset()
	d.cur = d.prev
	d.prevEnh = d.prev
	d.rng = noise{noiseSeed}
}

// DecodeP25P2 decodes one 72-bit P25 Phase 2 voice codeword into 160
// samples of 8 kHz PCM. vcw holds 72 bits, one per byte (0 or 1), in the
// order OP25's p25p2_tdma.cc hands them to p25p2_vf::process_vcw (i.e. as
// extracted from the descrambled 4V/2V burst). errs is the number of bit
// errors the Golay decoders corrected (or a large number if uncorrectable);
// on an uncorrectable frame the decoder should do what mbelib does (repeat /
// mute per its error-handling rules) rather than emit garbage.
//
// Concretely: OP25 passes 36 dibits; vcw[2k] is dibit k's high bit and
// vcw[2k+1] its low bit. errs counts every bit the c0 Golay(24,12) and c1
// Golay(23,12) decoders flipped, or is Uncorrectable when c0 has four
// errors. Frames with more than three corrected data bits, or an
// uncorrectable c0, repeat the previous frame; the fourth repeat in a row
// mutes and resets.
func (d *Decoder) DecodeP25P2(vcw []byte) (pcm [160]int16, errs int) {
	r := decodeP25P2FEC(deinterleaveP25P2(vcw))
	return d.decode(r.frame, r.mbeErrs > badFrameErrs), r.errs
}

// DecodeP25P2Dibits is DecodeP25P2 for the 36-dibit form OP25 itself uses
// (values 0..3, first dibit first).
func (d *Decoder) DecodeP25P2Dibits(dibits []byte) (pcm [160]int16, errs int) {
	_ = dibits[35]
	var vcw [72]byte
	for k, v := range dibits[:36] {
		vcw[2*k], vcw[2*k+1] = v>>1&1, v&1
	}
	return d.DecodeP25P2(vcw[:])
}

// DecodeFrame synthesises an already error-corrected frame. bad marks it as
// untrustworthy, which repeats the previous frame instead (as DecodeP25P2
// does for frames with too many errors).
func (d *Decoder) DecodeFrame(f Frame, bad bool) [160]int16 {
	return d.decode(f, bad)
}

// Erasure produces the audio for a missing frame: the previous frame
// repeated, muting after three in a row. Silence-safe helper used when a
// burst is missing.
func (d *Decoder) Erasure() [160]int16 {
	d.cur.repeat = d.prev.repeat
	return d.repeatOrMute()
}

// Params returns the model parameters of the most recently decoded frame,
// before spectral enhancement (after a mute, the reset state).
func (d *Decoder) Params() Params { return d.prev.snapshot() }

// decode is mbe_processAmbe2450Dataf.
func (d *Decoder) decode(f Frame, bad bool) [160]int16 {
	b := f.B()
	// Dequantise even a bad frame, as mbelib does: it extends prev's
	// magnitude tail, which later frames read.
	kind := dequantize(&b, &d.cur, &d.prev)
	if kind != kindVoice {
		// Erasure and tone frames: mbelib emits silence and restarts.
		return d.mute()
	}
	if bad {
		return d.repeatOrMute()
	}
	d.cur.repeat = 0
	return d.synth()
}

func (d *Decoder) repeatOrMute() [160]int16 {
	d.cur = d.prev
	d.cur.repeat++
	if d.cur.repeat > maxRepeats {
		return d.mute()
	}
	return d.synth()
}

func (d *Decoder) synth() [160]int16 {
	d.prev = d.cur
	enhance(&d.cur)
	synthesize(&d.buf, &d.cur, &d.prevEnh, &d.rng)
	d.prevEnh = d.cur
	return toPCM(&d.buf)
}

// mute emits silence and resets the parameter state (not the noise
// generator, which mbelib's rand() never rewinds either).
func (d *Decoder) mute() [160]int16 {
	d.prev.reset()
	d.cur = d.prev
	d.prevEnh = d.prev
	return [160]int16{}
}
