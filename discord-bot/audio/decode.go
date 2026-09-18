package audio

import (
	"fmt"

	"github.com/disgoorg/disgo/voice"
	"layeh.com/gopus"
)

// maxSilenceFill caps the pause Decode will reinsert. A timestamp gap longer
// than this is a reordered or bogus packet rather than a pause, so one bad
// header cannot make us allocate minutes of silence.
const maxSilenceFill = 5 * SampleRate

// Decoder turns one speaker's packets into mono PCM at SampleRate.
//
// Discord clients stop transmitting through silence rather than sending quiet
// frames, so the gap between RTP timestamps is the only record that a pause
// happened. Decode reinserts it: left unfilled, the words butt up against each
// other and a model hears a sentence nobody said.
//
// One Decoder serves one stream. Opus predicts across frames, and the gap
// tracking assumes a single timestamp line.
type Decoder struct {
	opus     *gopus.Decoder
	started  bool
	lastTime uint32
}

func NewDecoder() (*Decoder, error) {
	opus, err := gopus.NewDecoder(SampleRate, Channels)
	if err != nil {
		return nil, fmt.Errorf("creating opus decoder: %w", err)
	}
	return &Decoder{opus: opus}, nil
}

// Decode returns the packet's audio as mono, and how many samples of pause
// preceded it since the previous packet. The slice is fresh on every call.
func (d *Decoder) Decode(packet *voice.Packet) (frame []int16, silence int, err error) {
	stereo, err := d.opus.Decode(packet.Opus, FrameSize, false)
	if err != nil {
		return nil, 0, err
	}

	// Timestamps count samples, so subtracting the last one and the frame it
	// covered leaves the samples nobody sent. uint32 arithmetic handles the
	// clock wrapping.
	if d.started {
		if elapsed := int(packet.Timestamp - d.lastTime); elapsed > FrameSize && elapsed-FrameSize < maxSilenceFill {
			silence = elapsed - FrameSize
		}
	}
	d.started = true
	d.lastTime = packet.Timestamp

	// Both channels carry the same voice, so mono halves the output and costs
	// nothing a model would have used.
	frame = make([]int16, len(stereo)/Channels)
	for i := range frame {
		frame[i] = int16((int32(stereo[i*Channels]) + int32(stereo[i*Channels+1])) / 2)
	}
	return frame, silence, nil
}
