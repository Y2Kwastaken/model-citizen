package audio

import (
	sharedaudio "github.com/Y2Kwastaken/model-citizen/shared/audio"
	"gopkg.in/hraban/opus.v2"
)

type OpusMixer struct {
	sharedaudio.Mixer
	encoder *opus.Encoder
	out     []byte
}

// gobbles up a root mixer and utilizes it as a root handleer
// naturally calibrated to discord's opus specs
func NewOpusMixer(mixer sharedaudio.Mixer) (*OpusMixer, error) {
	enc, err := opus.NewEncoder(48000, 2, opus.AppAudio)
	if err != nil {
		return &OpusMixer{}, err
	}

	return &OpusMixer{
		Mixer:   mixer,
		encoder: enc,
		out:     make([]byte, 4000),
	}, nil
}

func (o *OpusMixer) ProvideOpusFrame() ([]byte, error) {
	pcm, err := o.Mixer.NextFrames()
	if err != nil || pcm == nil {
		return nil, err
	}

	pcmInts := sharedaudio.ByteToInt16(pcm)
	n, err := o.encoder.Encode(pcmInts, o.out)
	if err != nil {
		return nil, err
	}

	return o.out[:n], nil
}

func (o *OpusMixer) Close() {
	o.CloseLayers()
}
