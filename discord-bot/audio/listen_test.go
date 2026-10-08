// claude authored, kept separate from the hand written audio code

package audio

import (
	"encoding/binary"
	"math"
	"os"
	"testing"
	"time"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/wake"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
	"gopkg.in/hraban/opus.v2"
)

// needs libonnxruntime.so 1.29.x from ONNXRUNTIME_LIB, skipped without it
func testListener(t *testing.T) *OpusListener {
	t.Helper()
	library := os.Getenv("ONNXRUNTIME_LIB")
	if library == "" {
		t.Skip("set ONNXRUNTIME_LIB to run the listener tests")
	}

	model, err := wake.Load(library, "../assets/wake", "hey_model.onnx")
	if err != nil {
		t.Fatal(err)
	}
	return NewOpusListener(model, 0.5, 3*time.Second, 30*time.Second, -40)
}

// a 16 kHz mono wav as the 20 ms opus packets a discord client would send
func packets(t *testing.T, path string) []*voice.Packet {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// the test clips have a plain 44 byte header
	body := raw[44:]

	// back up to 48 kHz stereo by repeating each sample, the listener's low
	// pass removes what that adds above 8 kHz
	var stereo []int16
	for i := 0; i+1 < len(body); i += 2 {
		sample := int16(binary.LittleEndian.Uint16(body[i:]))
		for range 3 * channels {
			stereo = append(stereo, sample)
		}
	}

	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		t.Fatal(err)
	}

	frame := sampleRate / 50 * channels
	var out []*voice.Packet
	for i := 0; i+frame <= len(stereo); i += frame {
		buf := make([]byte, 4000)
		n, err := enc.Encode(stereo[i:i+frame], buf)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, &voice.Packet{
			Sequence:  uint16(len(out)),
			Timestamp: uint32(len(out) * sampleRate / 50),
			Opus:      buf[:n],
		})
	}
	return out
}

func send(t *testing.T, l *OpusListener, user snowflake.ID, packets []*voice.Packet) {
	t.Helper()
	for _, packet := range packets {
		if err := l.ReceiveOpusFrame(user, packet); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListenerHearsTheWakeWord(t *testing.T) {
	l := testListener(t)
	send(t, l, 7, packets(t, "../wake/testdata/hey_model_positive.wav"))

	select {
	case wake := <-l.Wakes():
		if wake.User != 7 {
			t.Fatalf("woken by user %d, want 7", wake.User)
		}
	default:
		t.Fatal("the wake word wasn't heard")
	}
}

func TestListenerIgnoresOtherSpeech(t *testing.T) {
	l := testListener(t)
	send(t, l, 7, packets(t, "../wake/testdata/hey_model_negative.wav"))

	select {
	case <-l.Wakes():
		t.Fatal("woke on the negative clip")
	default:
	}
}

// audio disgo hasn't matched to a user is dropped
func TestListenerIgnoresUnknownUsers(t *testing.T) {
	l := testListener(t)
	send(t, l, 0, packets(t, "../wake/testdata/hey_model_positive.wav"))

	select {
	case <-l.Wakes():
		t.Fatal("woke on audio from user 0")
	default:
	}
}

// saying it twice in a row only wakes once inside the debounce
func TestListenerDebounces(t *testing.T) {
	l := testListener(t)
	clip := packets(t, "../wake/testdata/hey_model_positive.wav")
	send(t, l, 7, clip)

	// the same clip again, timestamps carrying on after a short pause
	offset := uint32(len(clip)*sampleRate/50 + sampleRate/2)
	for _, packet := range clip {
		again := *packet
		again.Timestamp += offset
		if err := l.ReceiveOpusFrame(7, &again); err != nil {
			t.Fatal(err)
		}
	}

	if got := len(l.Wakes()); got != 1 {
		t.Fatalf("woke %d times, want 1", got)
	}
}

// a speaker who left starts fresh, and Close forgets everyone
func TestListenerForgets(t *testing.T) {
	l := testListener(t)
	send(t, l, 7, packets(t, "../wake/testdata/hey_model_negative.wav")[:5])
	send(t, l, 8, packets(t, "../wake/testdata/hey_model_negative.wav")[:5])

	l.CleanupUser(7)
	if _, ok := l.speakers[7]; ok {
		t.Fatal("user 7 is still remembered after leaving")
	}

	l.Close()
	if len(l.speakers) != 0 {
		t.Fatalf("%d speakers remembered after close", len(l.speakers))
	}
}

// shifts every packet's timestamp, as if the speaker paused before it
func later(packets []*voice.Packet, by uint32) []*voice.Packet {
	out := make([]*voice.Packet, len(packets))
	for i, packet := range packets {
		moved := *packet
		moved.Timestamp += by
		out[i] = &moved
	}
	return out
}

// a long pause splits one person's speech in two, a short one doesn't
func TestUtterancesSplitOnPauses(t *testing.T) {
	l := testListener(t)
	clip := packets(t, "../wake/testdata/hey_model_negative.wav")
	clipLength := uint32(len(clip) * sampleRate / 50)

	start := time.Now()
	send(t, l, 7, clip)
	// 200 ms later, the same utterance
	send(t, l, 7, later(clip, clipLength+sampleRate/5))
	// a second later again, a new utterance
	send(t, l, 7, later(clip, 2*clipLength+sampleRate/5+sampleRate))
	// someone else talking
	send(t, l, 8, clip)

	got := l.Utterances(start, time.Now())
	if len(got) != 3 {
		t.Fatalf("got %d utterances, want 3", len(got))
	}

	clipTime := time.Duration(len(clip)) * 20 * time.Millisecond
	var sevens []Utterance
	for _, u := range got {
		if u.User == 7 {
			sevens = append(sevens, u)
		}
	}
	if len(sevens) != 2 {
		t.Fatalf("user 7 has %d utterances, want 2", len(sevens))
	}
	// two clips and the 200 ms pause between them
	if d := sevens[0].Duration(); d < 2*clipTime || d > 2*clipTime+300*time.Millisecond {
		t.Errorf("first utterance is %s, want about %s", d, 2*clipTime+200*time.Millisecond)
	}
	if d := sevens[1].Duration(); d < clipTime-50*time.Millisecond || d > clipTime+50*time.Millisecond {
		t.Errorf("second utterance is %s, want about %s", d, clipTime)
	}

	for i := 1; i < len(got); i++ {
		if got[i].Start.Before(got[i-1].Start) {
			t.Fatal("utterances aren't oldest first")
		}
	}
}

// only audio that arrived between from and until is returned
func TestUtterancesWindow(t *testing.T) {
	l := testListener(t)
	clip := packets(t, "../wake/testdata/hey_model_negative.wav")
	send(t, l, 7, clip)
	after := time.Now()

	if got := l.Utterances(after, time.Now()); len(got) != 0 {
		t.Fatalf("got %d utterances from after the speech ended", len(got))
	}
	if got := l.Utterances(after.Add(-time.Minute), after); len(got) != 1 {
		t.Fatalf("got %d utterances covering the speech, want 1", len(got))
	}
}

func TestLastHeard(t *testing.T) {
	l := testListener(t)
	if _, ok := l.LastHeard(7); ok {
		t.Fatal("user 7 heard before speaking")
	}

	before := time.Now()
	send(t, l, 7, packets(t, "../wake/testdata/hey_model_negative.wav")[:5])
	heard, ok := l.LastHeard(7)
	if !ok || heard.Before(before) {
		t.Fatalf("last heard %s, want after %s", heard, before)
	}
}

// quiet packets that keep arriving after speech count as heard but not loud
func TestLastLoudIgnoresBackgroundNoise(t *testing.T) {
	l := testListener(t)
	clip := packets(t, "../wake/testdata/hey_model_positive.wav")
	send(t, l, 7, clip)
	loud, _ := l.LastLoud(7)
	if loud.IsZero() {
		t.Fatal("the speech never counted as loud")
	}

	// faint hiss, well under -40 dBFS, carrying on after the speech
	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		t.Fatal(err)
	}
	noise := make([]int16, sampleRate/50*channels)
	for i := range noise {
		noise[i] = int16((i*7919)%61 - 30)
	}
	start := uint32(len(clip) * sampleRate / 50)
	time.Sleep(20 * time.Millisecond)
	for i := range 20 {
		buf := make([]byte, 4000)
		n, err := enc.Encode(noise, buf)
		if err != nil {
			t.Fatal(err)
		}
		packet := &voice.Packet{Timestamp: start + uint32(i*sampleRate/50), Opus: buf[:n]}
		if err := l.ReceiveOpusFrame(7, packet); err != nil {
			t.Fatal(err)
		}
	}

	heard, _ := l.LastHeard(7)
	after, _ := l.LastLoud(7)
	if !heard.After(loud) {
		t.Fatal("the noise packets weren't heard")
	}
	if !after.Equal(loud) {
		t.Fatalf("background noise moved last loud from %s to %s", loud, after)
	}
}

func TestLevel(t *testing.T) {
	if got := level(make([]int16, 960)); got != -100 {
		t.Fatalf("silence is %v dBFS, want -100", got)
	}
	full := make([]int16, 960)
	for i := range full {
		full[i] = 32767
		if i%2 == 1 {
			full[i] = -32768
		}
	}
	if got := level(full); got < -0.1 {
		t.Fatalf("full scale is %v dBFS, want 0", got)
	}
	half := make([]int16, 960)
	for i := range half {
		half[i] = 16384
	}
	if got := level(half); math.Abs(got+6.02) > 0.1 {
		t.Fatalf("half scale is %v dBFS, want about -6", got)
	}
}
