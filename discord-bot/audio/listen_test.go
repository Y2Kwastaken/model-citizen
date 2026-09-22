package audio

import (
	"math"
	"testing"
	"time"

	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
	"layeh.com/gopus"
)

// clock is a fake time source that advances one packet interval per call.
type clock struct{ t time.Time }

func (c *clock) now() time.Time {
	c.t = c.t.Add(FrameLength)
	return c.t
}

func newTestListener(wake *WakeWord, window time.Duration) (*Listener, *clock) {
	// the clock moves 20ms a packet, so a 3s clip replayed lands 3s later
	l := NewListener(wake, window, 0.5, 10*time.Second)
	c := &clock{t: time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)}
	l.now = c.now
	return l, c
}

// encodePackets turns 16kHz mono into the packets Discord would carry it
// in: upsampled to 48kHz stereo and opus-encoded 20ms at a time.
func encodePackets(t *testing.T, pcm16 []int16) []*voice.Packet {
	t.Helper()
	return encodeStereo(t, upsample(pcm16, 0))
}

// encodePacketsWithHiss is encodePackets with band noise above 8kHz mixed
// in, which a box-filter decimator would fold into the speech band.
func encodePacketsWithHiss(t *testing.T, pcm16 []int16) []*voice.Packet {
	t.Helper()
	return encodeStereo(t, upsample(pcm16, 3000))
}

// upsample repeats each sample three times, adding a 14kHz tone of the given
// amplitude: content that only exists at 48kHz.
func upsample(pcm16 []int16, hiss float64) []int16 {
	stereo := make([]int16, len(pcm16)*decimation*Channels)
	for i, s := range pcm16 {
		for j := range decimation {
			n := i*decimation + j
			v := float64(s) + hiss*math.Sin(2*math.Pi*14000*float64(n)/SampleRate)
			stereo[n*Channels] = int16(math.Max(-32768, math.Min(32767, v)))
			stereo[n*Channels+1] = stereo[n*Channels]
		}
	}
	return stereo
}

func encodeStereo(t *testing.T, stereo []int16) []*voice.Packet {
	t.Helper()
	encoder, err := gopus.NewEncoder(SampleRate, Channels, gopus.Audio)
	if err != nil {
		t.Fatal(err)
	}
	var packets []*voice.Packet
	for i := 0; i+FrameSize*Channels <= len(stereo); i += FrameSize * Channels {
		opus, err := encoder.Encode(stereo[i:i+FrameSize*Channels], FrameSize, maxEncodedFrame)
		if err != nil {
			t.Fatal(err)
		}
		packets = append(packets, &voice.Packet{Timestamp: uint32(i / Channels), Opus: opus})
	}
	return packets
}

func TestListenerHearsTheWakeWord(t *testing.T) {
	l, _ := newTestListener(loadWakeWord(t), 10*time.Second)
	for _, p := range encodePackets(t, readWAV(t, "testdata/hey_model_positive.wav")) {
		if err := l.ReceiveOpusFrame(7, p); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case trig := <-l.Triggers():
		if trig.User != 7 || trig.Score < 0.5 {
			t.Fatalf("trigger = %+v", trig)
		}
	default:
		t.Fatal("the wake word went unheard through opus")
	}

	// the same audio again inside the debounce window is not a second trigger
	for _, p := range encodePackets(t, readWAV(t, "testdata/hey_model_positive.wav")) {
		_ = l.ReceiveOpusFrame(7, p)
	}
	select {
	case trig := <-l.Triggers():
		t.Fatalf("retriggered within the debounce: %+v", trig)
	default:
	}
}

func TestDebounceIsPerSpeaker(t *testing.T) {
	// The debounce stops one person's wake becoming several. It is not a
	// reason to go deaf to everyone else in the channel, so the second
	// speaker wakes it inside the first speaker's window.
	l, _ := newTestListener(loadWakeWord(t), 10*time.Second)
	for _, user := range []snowflake.ID{7, 8} {
		for _, p := range encodePackets(t, readWAV(t, "testdata/hey_model_positive.wav")) {
			if err := l.ReceiveOpusFrame(user, p); err != nil {
				t.Fatal(err)
			}
		}
		select {
		case trig := <-l.Triggers():
			if trig.User != user {
				t.Fatalf("trigger = %+v, want user %d", trig, user)
			}
		default:
			t.Fatalf("user %d went unheard inside the other speaker's debounce", user)
		}
	}
}

func TestListenerIgnoresTheNegative(t *testing.T) {
	l, _ := newTestListener(loadWakeWord(t), 10*time.Second)
	for _, p := range encodePackets(t, readWAV(t, "testdata/hey_model_negative.wav")) {
		_ = l.ReceiveOpusFrame(7, p)
	}
	select {
	case trig := <-l.Triggers():
		t.Fatalf("triggered on the negative phrase: %+v", trig)
	default:
	}
}

func TestContextOrdersSpeakersByTime(t *testing.T) {
	l, c := newTestListener(nil, 10*time.Second)
	a, b := tonePackets(t, 6), tonePackets(t, 6)

	// A talks, then B, then A again, with a real pause between A's turns
	for _, p := range a[:3] {
		_ = l.ReceiveOpusFrame(1, p)
	}
	for _, p := range b[:3] {
		_ = l.ReceiveOpusFrame(2, p)
	}
	a[3].Timestamp = a[2].Timestamp + FrameSize + utteranceGap
	for _, p := range a[3:] {
		_ = l.ReceiveOpusFrame(1, p)
	}

	utts := l.Context(c.t, 10*time.Second)
	if len(utts) != 3 {
		t.Fatalf("%d utterances, want 3: %+v", len(utts), utts)
	}
	want := []snowflake.ID{1, 2, 1}
	for i, u := range utts {
		if u.User != want[i] {
			t.Errorf("utterance %d by %d, want %d", i, u.User, want[i])
		}
		if len(u.PCM) != 3*FrameSize {
			t.Errorf("utterance %d has %d samples, want %d", i, len(u.PCM), 3*FrameSize)
		}
	}
	if !utts[0].Start.Before(utts[1].Start) || !utts[1].Start.Before(utts[2].Start) {
		t.Error("utterances are not in time order")
	}
}

func TestContextFillsShortPausesAndSplitsLongOnes(t *testing.T) {
	l, c := newTestListener(nil, 10*time.Second)
	p := tonePackets(t, 4)
	p[1].Timestamp = p[0].Timestamp + FrameSize + FrameSize // 20ms pause: filled
	p[2].Timestamp = p[1].Timestamp + FrameSize + utteranceGap
	p[3].Timestamp = p[2].Timestamp + FrameSize
	for _, packet := range p {
		_ = l.ReceiveOpusFrame(1, packet)
	}

	utts := l.Context(c.t, 10*time.Second)
	if len(utts) != 2 {
		t.Fatalf("%d utterances, want 2", len(utts))
	}
	if len(utts[0].PCM) != 3*FrameSize {
		t.Errorf("first utterance has %d samples, want %d (two frames and a filled pause)", len(utts[0].PCM), 3*FrameSize)
	}
	if !isSilent(utts[0].PCM[FrameSize : 2*FrameSize]) {
		t.Error("the filled pause is not silent")
	}
	if len(utts[1].PCM) != 2*FrameSize {
		t.Errorf("second utterance has %d samples, want %d", len(utts[1].PCM), 2*FrameSize)
	}
}

func TestContextForgetsBeyondTheWindow(t *testing.T) {
	l, c := newTestListener(nil, 200*time.Millisecond) // eleven packets, the window is inclusive
	for _, p := range tonePackets(t, 30) {
		_ = l.ReceiveOpusFrame(1, p)
	}
	utts := l.Context(c.t, time.Minute)
	total := 0
	for _, u := range utts {
		total += len(u.PCM)
	}
	if total > 11*FrameSize {
		t.Fatalf("%d samples retained, want at most %d", total, 11*FrameSize)
	}
	if !l.speakers[1].lastHeard.Equal(c.t) {
		t.Error("lastHeard should be the latest packet")
	}
}

func TestListenerDropsUnattributedAudio(t *testing.T) {
	l, c := newTestListener(nil, time.Second)
	for _, p := range tonePackets(t, 3) {
		_ = l.ReceiveOpusFrame(0, p)
	}
	if utts := l.Context(c.t, time.Second); len(utts) != 0 {
		t.Fatalf("kept %d utterances from user 0", len(utts))
	}
}
