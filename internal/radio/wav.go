package radio

import (
	"encoding/binary"
	"errors"
)

// AudioEncoder turns a call's assembled PCM (mono signed 16-bit
// little-endian) into the file that gets stored. WAV is the only one today:
// production has no ffmpeg, and an 8 kHz WAV is only 16 kB/s. An Opus
// encoder can slot in here later; transcription always gets WAV regardless.
type AudioEncoder interface {
	Encode(pcm []byte, sampleRate int) (EncodedAudio, error)
}

type EncodedAudio struct {
	Data        []byte
	ContentType string
	Ext         string // with the dot, e.g. ".wav"
}

// WAVEncoder wraps PCM in a RIFF/WAVE header, nothing else.
type WAVEncoder struct{}

func (WAVEncoder) Encode(pcm []byte, sampleRate int) (EncodedAudio, error) {
	if len(pcm)%2 != 0 {
		return EncodedAudio{}, errors.New("wav: odd number of PCM bytes")
	}
	return EncodedAudio{Data: EncodeWAV(pcm, sampleRate), ContentType: "audio/wav", Ext: ".wav"}, nil
}

// EncodeWAV returns a canonical 44-byte-header PCM WAV file: one channel,
// 16 bits per sample, at sampleRate.
func EncodeWAV(pcm []byte, sampleRate int) []byte {
	const (
		channels      = 1
		bitsPerSample = 16
		blockAlign    = channels * bitsPerSample / 8
	)
	out := make([]byte, 44+len(pcm))
	le := binary.LittleEndian
	copy(out[0:], "RIFF")
	le.PutUint32(out[4:], uint32(36+len(pcm)))
	copy(out[8:], "WAVE")
	copy(out[12:], "fmt ")
	le.PutUint32(out[16:], 16) // fmt chunk size
	le.PutUint16(out[20:], 1)  // PCM
	le.PutUint16(out[22:], channels)
	le.PutUint32(out[24:], uint32(sampleRate))
	le.PutUint32(out[28:], uint32(sampleRate*blockAlign)) // byte rate
	le.PutUint16(out[32:], blockAlign)
	le.PutUint16(out[34:], bitsPerSample)
	copy(out[36:], "data")
	le.PutUint32(out[40:], uint32(len(pcm)))
	copy(out[44:], pcm)
	return out
}

// pcmDurationMS is how long n bytes of 16-bit mono PCM play for, in ms.
func pcmDurationMS(n, sampleRate int) int {
	return int(int64(n/2) * 1000 / int64(sampleRate))
}
