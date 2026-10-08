// claude authored, kept separate from the hand written audio code

package audio

import (
	"encoding/binary"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/wake"
)

// wraps 16 kHz mono pcm in a 44 byte wav header for speech to text
func EncodeWAV(pcm []int16) []byte {
	const (
		bitsPerSample = 16
		blockAlign    = bitsPerSample / 8
		byteRate      = wake.SampleRate * blockAlign
	)
	size := len(pcm) * blockAlign

	out := make([]byte, 0, 44+size)
	out = append(out, "RIFF"...)
	out = binary.LittleEndian.AppendUint32(out, uint32(36+size))
	out = append(out, "WAVE"...)

	out = append(out, "fmt "...)
	out = binary.LittleEndian.AppendUint32(out, 16) // size of this chunk
	out = binary.LittleEndian.AppendUint16(out, 1)  // pcm
	out = binary.LittleEndian.AppendUint16(out, 1)  // mono
	out = binary.LittleEndian.AppendUint32(out, wake.SampleRate)
	out = binary.LittleEndian.AppendUint32(out, byteRate)
	out = binary.LittleEndian.AppendUint16(out, blockAlign)
	out = binary.LittleEndian.AppendUint16(out, bitsPerSample)

	out = append(out, "data"...)
	out = binary.LittleEndian.AppendUint32(out, uint32(size))
	for _, sample := range pcm {
		out = binary.LittleEndian.AppendUint16(out, uint16(sample))
	}

	return out
}
