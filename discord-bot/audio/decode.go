// claude authored, kept separate from the hand written audio code

package audio

import (
	"github.com/disgoorg/disgo/voice"
	"gopkg.in/hraban/opus.v2"
)

const (
	sampleRate = 48000
	channels   = 2
	// the most audio one opus packet can hold, 120 ms
	maxPacketSamples = sampleRate * 120 / 1000
	// a timestamp gap longer than this is a bad packet rather than a pause
	maxSilence = 5 * sampleRate
)

// one speaker's opus decoder. discord stops sending while someone is quiet,
// so the gap between packet timestamps is the only sign of a pause
type decoder struct {
	opus   *opus.Decoder
	stereo []int16
	// timestamp of the previous packet
	last    uint32
	started bool
}

func newDecoder() (*decoder, error) {
	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		return nil, err
	}

	return &decoder{
		opus:   dec,
		stereo: make([]int16, maxPacketSamples*channels),
	}, nil
}

// returns the packet as 48 kHz mono and how many samples of silence came before it
func (d *decoder) decode(packet *voice.Packet) ([]int16, int, error) {
	n, err := d.opus.Decode(packet.Opus, d.stereo)
	if err != nil {
		return nil, 0, err
	}

	// timestamps count samples, uint32 subtraction handles them wrapping
	silence := 0
	if d.started {
		gap := int(packet.Timestamp-d.last) - n
		if gap > 0 && gap < maxSilence {
			silence = gap
		}
	}
	d.started = true
	d.last = packet.Timestamp

	// both channels carry the same voice
	mono := make([]int16, n)
	for i := range mono {
		mono[i] = int16((int32(d.stereo[i*channels]) + int32(d.stereo[i*channels+1])) / 2)
	}

	return mono, silence, nil
}
