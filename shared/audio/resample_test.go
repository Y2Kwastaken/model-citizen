// claude authored, kept separate from the hand written audio code

package audio

import (
	"math"
	"testing"
)

// one second of a sine wave at 48 kHz
func tone(hz float64) []int16 {
	out := make([]int16, 48000)
	for i := range out {
		out[i] = int16(8000 * math.Sin(2*math.Pi*hz*float64(i)/48000))
	}
	return out
}

func loudest(pcm []int16) int16 {
	var best int16
	// skip the filter warming up
	for _, sample := range pcm[100:] {
		best = max(best, sample)
	}
	return best
}

// fed whole or a 20 ms packet at a time, the output must be identical
func TestDownsampleStreams(t *testing.T) {
	in := tone(440)
	whole := NewDownsampler().Downsample(in)

	pieces := NewDownsampler()
	var chunked []int16
	for i := 0; i < len(in); i += 960 {
		chunked = append(chunked, pieces.Downsample(in[i:i+960])...)
	}

	if len(whole) != 16000 || len(chunked) != len(whole) {
		t.Fatalf("got %d whole and %d chunked samples, want 16000", len(whole), len(chunked))
	}
	for i := range whole {
		if whole[i] != chunked[i] {
			t.Fatalf("sample %d: whole %d chunked %d", i, whole[i], chunked[i])
		}
	}
}

// speech band audio comes through at full volume
func TestDownsampleKeepsSpeech(t *testing.T) {
	if peak := loudest(NewDownsampler().Downsample(tone(440))); peak < 7500 {
		t.Fatalf("440 Hz peaked at %d, want about 8000", peak)
	}
}

// 13 kHz would fold down to 3 kHz without the low pass
func TestDownsampleBlocksAliasing(t *testing.T) {
	if peak := loudest(NewDownsampler().Downsample(tone(13000))); peak > 400 {
		t.Fatalf("13 kHz came through at %d, want under 400", peak)
	}
}
