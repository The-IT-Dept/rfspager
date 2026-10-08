package p25

// Stats counts what the decoder has seen since it started.
type Stats struct {
	Frames   int // frames with a valid NID
	Blocks   int // TSBK blocks attempted
	BlocksOK int // ... that passed CRC
	NAC      uint16
}

// Decoder is the whole control-channel receive chain for one 48 kS/s
// channel: demodulator, framer and TSBK parser.
type Decoder struct {
	demod *Demod
	stats Stats

	// OnMessage receives every decoded message, in order.
	OnMessage func(Message)
}

func NewDecoder(onMessage func(Message)) *Decoder {
	d := &Decoder{OnMessage: onMessage}
	d.demod = NewDemod(NewFramer(d.frame))
	return d
}

func (d *Decoder) frame(f Frame) {
	d.stats.Frames++
	d.stats.NAC = f.NAC
	if f.DUID != DUIDTSBK {
		return
	}
	for _, b := range f.Blocks {
		d.stats.Blocks++
		if !b.CRCOK {
			continue
		}
		d.stats.BlocksOK++
		for _, m := range parseTSBK(b.Data).Messages() {
			d.OnMessage(m)
		}
	}
}

// Process decodes a block of 48 kS/s baseband.
func (d *Decoder) Process(baseband []complex128) { d.demod.Process(baseband) }

// ProcessCS8 decodes interleaved signed 8-bit IQ at 48 kS/s.
func (d *Decoder) ProcessCS8(iq []byte) { d.demod.ProcessCS8(iq) }

func (d *Decoder) Stats() Stats { return d.stats }

// FreqOffsetHz is the AFC's estimate of the tuning error.
func (d *Decoder) FreqOffsetHz() float64 { return d.demod.FreqOffsetHz() }
