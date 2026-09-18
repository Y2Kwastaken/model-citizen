package audio

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
	"time"
)

// tone is n frames of a constant sample value, as s16le stereo.
func tone(frames int, value int16) []byte {
	out := make([]byte, frames*FrameBytes)
	for i := 0; i+1 < len(out); i += 2 {
		binary.LittleEndian.PutUint16(out[i:], uint16(value))
	}
	return out
}

func sampleAt(b []byte, i int) int16 {
	return int16(binary.LittleEndian.Uint16(b[i*2:]))
}

type nopCloser struct{ io.Reader }

func (nopCloser) Close() error { return nil }

func clip(b []byte) io.ReadCloser { return nopCloser{bytes.NewReader(b)} }

func TestMixerPassesTheBedThroughUntouched(t *testing.T) {
	bed := tone(2, 1000)
	mixer := NewMixer(bytes.NewReader(bed))

	got, err := io.ReadAll(mixer)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, bed) {
		t.Fatalf("read %d bytes, want the bed back unchanged", len(got))
	}
}

// Nothing playing: the mixer exists only to carry the clip, and ends with it.
func TestMixerSpeaksWithNoBed(t *testing.T) {
	said := tone(2, 500)
	mixer := NewMixer(nil)

	done, ok := mixer.Say(clip(said))
	if !ok {
		t.Fatal("Say refused a fresh mixer")
	}

	got, err := io.ReadAll(mixer)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, said) {
		t.Fatalf("read %d bytes, want the clip", len(got))
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the clip never reported itself finished")
	}
}

func TestMixerDucksTheBedUnderSpeech(t *testing.T) {
	mixer := NewMixer(bytes.NewReader(tone(2, 1000)))
	// one frame of speech over a two frame bed
	if _, ok := mixer.Say(clip(tone(1, 100))); !ok {
		t.Fatal("Say refused a fresh mixer")
	}

	got, err := io.ReadAll(mixer)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 2*FrameBytes {
		t.Fatalf("read %d bytes, want %d", len(got), 2*FrameBytes)
	}

	// while talking: the bed ducked, plus the speech
	if want := int16(1000*duckedGain) + 100; sampleAt(got, 0) != want {
		t.Errorf("mixed sample = %d, want %d", sampleAt(got, 0), want)
	}
	// after: the bed at full volume again
	if want := int16(1000); sampleAt(got, FrameSize*Channels) != want {
		t.Errorf("sample after the clip = %d, want %d", sampleAt(got, FrameSize*Channels), want)
	}
}

// The bot should finish its sentence even if the song runs out underneath it.
func TestMixerKeepsTalkingPastTheEndOfTheBed(t *testing.T) {
	mixer := NewMixer(bytes.NewReader(tone(1, 1000)))
	if _, ok := mixer.Say(clip(tone(3, 500))); !ok {
		t.Fatal("Say refused a fresh mixer")
	}

	got, err := io.ReadAll(mixer)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 3*FrameBytes {
		t.Fatalf("read %d bytes, want the clip to outlive the bed", len(got))
	}
	if want := int16(500); sampleAt(got, 2*FrameSize*Channels) != want {
		t.Errorf("last frame = %d, want the speech alone", sampleAt(got, 2*FrameSize*Channels))
	}
}

func TestMixerPlaysQueuedClipsInOrder(t *testing.T) {
	mixer := NewMixer(nil)
	mixer.Say(clip(tone(1, 100)))
	mixer.Say(clip(tone(1, 200)))

	got, err := io.ReadAll(mixer)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 2*FrameBytes {
		t.Fatalf("read %d bytes, want both clips", len(got))
	}
	if sampleAt(got, 0) != 100 || sampleAt(got, FrameSize*Channels) != 200 {
		t.Errorf("clips played out of order: %d then %d", sampleAt(got, 0), sampleAt(got, FrameSize*Channels))
	}
}

// A spent mixer must refuse, or the line would be queued where nothing reads.
func TestMixerRefusesOnceSpent(t *testing.T) {
	mixer := NewMixer(nil)
	if _, err := io.ReadAll(mixer); err != nil {
		t.Fatalf("read: %v", err)
	}

	if _, ok := mixer.Say(clip(tone(1, 100))); ok {
		t.Fatal("a finished mixer accepted a clip")
	}
}

func TestMixerCloseWakesAnAbandonedClip(t *testing.T) {
	mixer := NewMixer(bytes.NewReader(tone(100, 1000)))
	done, _ := mixer.Say(clip(tone(1, 100)))

	if err := mixer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("closing left a waiter hanging")
	}

	if _, ok := mixer.Say(clip(tone(1, 100))); ok {
		t.Fatal("a closed mixer accepted a clip")
	}
}

// Speech is louder than the headroom left in a loud track, so the sum has to
// clamp rather than wrap into a click.
func TestMixdownClamps(t *testing.T) {
	bed := tone(1, 32000)
	mixdown(bed, tone(1, 32000))
	if got := sampleAt(bed, 0); got != 32767 {
		t.Errorf("clamped sample = %d, want 32767", got)
	}

	bed = tone(1, -32000)
	mixdown(bed, tone(1, -32000))
	if got := sampleAt(bed, 0); got != -32768 {
		t.Errorf("clamped sample = %d, want -32768", got)
	}
}
