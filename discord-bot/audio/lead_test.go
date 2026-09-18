package audio

import (
	"sync/atomic"
	"testing"
	"time"
)

// endless is a bed that never runs out, counting what has been taken from it.
type endless struct{ read atomic.Int64 }

func (e *endless) Read(p []byte) (int, error) {
	clear(p)
	e.read.Add(int64(len(p)))
	return len(p), nil
}

// The encoder runs ahead of playback so a stalled ffmpeg cannot break the
// stream. That lead is also how long after being asked the bot is heard,
// because a line is mixed in behind whatever is already encoded -- so it has
// to stay short enough to answer into the conversation that asked.
func TestTheEncoderStaysCloseToPlayback(t *testing.T) {
	bed := &endless{}
	mixer := NewMixer(bed)
	stream, err := NewOpusStream(mixer, mixer)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	// let the producer run as far ahead as it is willing to
	time.Sleep(300 * time.Millisecond)

	consumed := bed.read.Load()
	ahead := time.Duration(consumed/(Channels*2)) * time.Second / SampleRate
	t.Logf("the encoder is %s ahead of playback", ahead.Round(time.Millisecond))

	if ahead > 2*time.Second {
		t.Errorf("the bot would be heard %s after it was asked to speak", ahead.Round(time.Millisecond))
	}
	if ahead < prefillFrames*FrameLength {
		t.Errorf("only %s buffered, less than the prefill: a stalled ffmpeg would break the stream", ahead)
	}
}
