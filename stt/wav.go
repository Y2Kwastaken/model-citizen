package stt

import "encoding/binary"

// wavHeaderSize is the length of a canonical RIFF/PCM header: the three chunks
// written below, with no extensions.
const wavHeaderSize = 44

// WAV wraps 16-bit mono PCM in a RIFF header.
//
// The transcription endpoint takes compressed formats too, but WAV is 44 bytes
// in front of samples we already hold, where FLAC would be an encoder and a
// dependency to save bandwidth we are not short of. Revisit if the clips ever
// get long.
func WAV(pcm []int16, sampleRate int) []byte {
	const (
		channels      = 1
		bitsPerSample = 16
		bytesPerBlock = channels * bitsPerSample / 8
	)

	dataSize := len(pcm) * bytesPerBlock
	out := make([]byte, wavHeaderSize+dataSize)

	copy(out[0:], "RIFF")
	// Everything after this field, which is the whole file bar the leading
	// "RIFF" and the size itself.
	binary.LittleEndian.PutUint32(out[4:], uint32(wavHeaderSize-8+dataSize))
	copy(out[8:], "WAVE")

	copy(out[12:], "fmt ")
	binary.LittleEndian.PutUint32(out[16:], 16) // rest of the fmt chunk
	binary.LittleEndian.PutUint16(out[20:], 1)  // uncompressed PCM
	binary.LittleEndian.PutUint16(out[22:], channels)
	binary.LittleEndian.PutUint32(out[24:], uint32(sampleRate))
	binary.LittleEndian.PutUint32(out[28:], uint32(sampleRate*bytesPerBlock))
	binary.LittleEndian.PutUint16(out[32:], bytesPerBlock)
	binary.LittleEndian.PutUint16(out[34:], bitsPerSample)

	copy(out[36:], "data")
	binary.LittleEndian.PutUint32(out[40:], uint32(dataSize))

	for i, sample := range pcm {
		binary.LittleEndian.PutUint16(out[wavHeaderSize+i*bytesPerBlock:], uint16(sample))
	}

	return out
}
