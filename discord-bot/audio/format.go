package audio

import (
	"encoding/binary"
	"time"
)

// What Discord sends and expects: 20ms of 48kHz stereo per packet.
const (
	SampleRate = 48000
	Channels   = 2
	// FrameSize is samples per channel in one packet.
	FrameSize = SampleRate / 50
	// FrameBytes is FrameSize as interleaved s16le.
	FrameBytes  = FrameSize * Channels * 2
	FrameLength = 20 * time.Millisecond
)

// PCMFromBytes reads s16le into out, which must hold len(src)/2 samples.
//
// uint16 to int16 keeps the bit pattern, which is what signed 16-bit little
// endian already is.
func PCMFromBytes(src []byte, out []int16) {
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(src[i*2:]))
	}
}

// PCMToBytes writes samples as s16le.
func PCMToBytes(pcm []int16) []byte {
	out := make([]byte, len(pcm)*2)
	for i, sample := range pcm {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(sample))
	}
	return out
}
