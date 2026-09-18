package audio

import "encoding/binary"

// wavHeaderSize is a canonical RIFF/PCM header: three chunks, no extras.
const wavHeaderSize = 44

// EncodeWAV takes mono PCM at SampleRate down to WakeRate and wraps it in a
// RIFF header. It is what goes to the transcriber: 16kHz is what the
// Whisper-family models are trained on, and a wav costs no process spawn
// where a flac through ffmpeg cost ~100ms per utterance.
func EncodeWAV(pcm []int16) []byte {
	var d decimator
	samples := d.Downsample(pcm)

	const (
		channels      = 1
		bitsPerSample = 16
		blockAlign    = channels * bitsPerSample / 8
	)
	dataSize := len(samples) * blockAlign
	out := make([]byte, wavHeaderSize+dataSize)

	copy(out[0:], "RIFF")
	binary.LittleEndian.PutUint32(out[4:], uint32(wavHeaderSize-8+dataSize))
	copy(out[8:], "WAVE")
	copy(out[12:], "fmt ")
	binary.LittleEndian.PutUint32(out[16:], 16) // rest of the fmt chunk
	binary.LittleEndian.PutUint16(out[20:], 1)  // uncompressed PCM
	binary.LittleEndian.PutUint16(out[22:], channels)
	binary.LittleEndian.PutUint32(out[24:], WakeRate)
	binary.LittleEndian.PutUint32(out[28:], WakeRate*blockAlign)
	binary.LittleEndian.PutUint16(out[32:], blockAlign)
	binary.LittleEndian.PutUint16(out[34:], bitsPerSample)
	copy(out[36:], "data")
	binary.LittleEndian.PutUint32(out[40:], uint32(dataSize))
	copy(out[wavHeaderSize:], PCMToBytes(samples))
	return out
}
