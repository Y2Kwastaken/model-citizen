package audio

import (
	"math"
	"testing"
	"time"

	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
	"layeh.com/gopus"
)

// tonePackets encodes n frames of a 440Hz tone as consecutive packets, the
// way a client sends uninterrupted speech.
func tonePackets(t *testing.T, n int) []*voice.Packet {
	t.Helper()
	encoder, err := gopus.NewEncoder(SampleRate, Channels, gopus.Audio)
	if err != nil {
		t.Fatal(err)
	}

	packets := make([]*voice.Packet, 0, n)
	pcm := make([]int16, FrameSize*Channels)
	for frame := range n {
		for i := range FrameSize {
			sample := int16(8000 * math.Sin(2*math.Pi*440*float64(frame*FrameSize+i)/SampleRate))
			pcm[i*Channels], pcm[i*Channels+1] = sample, sample
		}
		opus, err := encoder.Encode(pcm, FrameSize, maxEncodedFrame)
		if err != nil {
			t.Fatal(err)
		}
		packets = append(packets, &voice.Packet{Timestamp: uint32(frame * FrameSize), Opus: opus})
	}
	return packets
}

func decodeAll(t *testing.T, packets []*voice.Packet) [][]int16 {
	t.Helper()
	decoder, err := NewDecoder()
	if err != nil {
		t.Fatal(err)
	}
	out := make([][]int16, 0, len(packets))
	for i, packet := range packets {
		mono, err := decoder.Decode(packet)
		if err != nil {
			t.Fatalf("packet %d: %v", i, err)
		}
		out = append(out, mono)
	}
	return out
}

func leadingSilence(pcm []int16) int {
	for i, sample := range pcm {
		if sample != 0 {
			return i
		}
	}
	return len(pcm)
}

func TestDecodeYieldsOneMonoFrame(t *testing.T) {
	for i, mono := range decodeAll(t, tonePackets(t, 3)) {
		if len(mono) != FrameSize {
			t.Fatalf("packet %d: %d samples, want %d", i, len(mono), FrameSize)
		}
		if leadingSilence(mono) == len(mono) {
			t.Fatalf("packet %d: decoded a tone to silence", i)
		}
	}
}

func TestDecodeFillsPauses(t *testing.T) {
	packets := tonePackets(t, 2)
	// The second packet arrives three frames after the first, so two frames
	// of silence went unsent.
	packets[1].Timestamp = packets[0].Timestamp + 3*FrameSize

	mono := decodeAll(t, packets)[1]
	if len(mono) != 3*FrameSize {
		t.Fatalf("%d samples, want %d: the pause should be reinserted", len(mono), 3*FrameSize)
	}
	if got := leadingSilence(mono); got < 2*FrameSize {
		t.Fatalf("audio starts at sample %d, want the first %d silent", got, 2*FrameSize)
	}
}

func TestDecodeIgnoresBogusGaps(t *testing.T) {
	packets := tonePackets(t, 2)
	packets[1].Timestamp = packets[0].Timestamp + maxSilenceFill + FrameSize

	if mono := decodeAll(t, packets)[1]; len(mono) != FrameSize {
		t.Fatalf("%d samples, want %d: a gap past maxSilenceFill is not a pause", len(mono), FrameSize)
	}
}

func TestDecodeFirstPacketHasNoGap(t *testing.T) {
	packets := tonePackets(t, 1)
	packets[0].Timestamp = 123456789

	if mono := decodeAll(t, packets)[0]; len(mono) != FrameSize {
		t.Fatalf("%d samples, want %d: nothing precedes the first packet", len(mono), FrameSize)
	}
}

func TestDecodeSurvivesTimestampWrap(t *testing.T) {
	packets := tonePackets(t, 2)
	packets[0].Timestamp = math.MaxUint32 - 100
	packets[1].Timestamp = packets[0].Timestamp + FrameSize // wraps

	if mono := decodeAll(t, packets)[1]; len(mono) != FrameSize {
		t.Fatalf("%d samples, want %d: a wrapped clock is still consecutive", len(mono), FrameSize)
	}
}

func TestRecorderOnlyHearsTargetAfterStart(t *testing.T) {
	const target, other = snowflake.ID(1), snowflake.ID(2)
	packets := tonePackets(t, 4)

	rec, err := NewRecorder(target, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	_ = rec.ReceiveOpusFrame(target, packets[0]) // before Start
	rec.Start()
	_ = rec.ReceiveOpusFrame(other, packets[1])
	_ = rec.ReceiveOpusFrame(target, packets[2])
	_ = rec.ReceiveOpusFrame(target, packets[3])

	pcm, frames := rec.Stop()
	if frames != 2 {
		t.Fatalf("frames = %d, want 2", frames)
	}
	if len(pcm) != 2*FrameSize {
		t.Fatalf("%d samples, want %d", len(pcm), 2*FrameSize)
	}
}

func TestRecorderStopsAtTheWindow(t *testing.T) {
	packets := tonePackets(t, 5)
	window := 3 * FrameLength

	rec, err := NewRecorder(1, window)
	if err != nil {
		t.Fatal(err)
	}
	rec.Start()
	for _, packet := range packets {
		_ = rec.ReceiveOpusFrame(1, packet)
	}

	pcm, _ := rec.Stop()
	if want := int(window.Seconds() * SampleRate); len(pcm) != want {
		t.Fatalf("%d samples, want %d", len(pcm), want)
	}
}

func TestRecorderIgnoresPacketsAfterStop(t *testing.T) {
	packets := tonePackets(t, 2)

	rec, err := NewRecorder(1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	rec.Start()
	_ = rec.ReceiveOpusFrame(1, packets[0])
	pcm, _ := rec.Stop()
	_ = rec.ReceiveOpusFrame(1, packets[1])

	if len(pcm) != FrameSize {
		t.Fatalf("%d samples, want %d: Stop should have closed the window", len(pcm), FrameSize)
	}
}
