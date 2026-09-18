package audio

import (
	"math"
	"testing"

	"github.com/disgoorg/disgo/voice"
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

type decoded struct {
	frame   []int16
	silence int
}

func decodeAll(t *testing.T, packets []*voice.Packet) []decoded {
	t.Helper()
	decoder, err := NewDecoder()
	if err != nil {
		t.Fatal(err)
	}
	out := make([]decoded, 0, len(packets))
	for i, packet := range packets {
		frame, silence, err := decoder.Decode(packet)
		if err != nil {
			t.Fatalf("packet %d: %v", i, err)
		}
		out = append(out, decoded{frame, silence})
	}
	return out
}

func isSilent(pcm []int16) bool {
	for _, sample := range pcm {
		if sample != 0 {
			return false
		}
	}
	return true
}

func TestDecodeYieldsOneMonoFrame(t *testing.T) {
	for i, d := range decodeAll(t, tonePackets(t, 3)) {
		if len(d.frame) != FrameSize {
			t.Fatalf("packet %d: %d samples, want %d", i, len(d.frame), FrameSize)
		}
		if d.silence != 0 {
			t.Fatalf("packet %d: reported a %d-sample pause between consecutive packets", i, d.silence)
		}
		if isSilent(d.frame) {
			t.Fatalf("packet %d: decoded a tone to silence", i)
		}
	}
}

func TestDecodeReportsPauses(t *testing.T) {
	packets := tonePackets(t, 2)
	// The second packet arrives three frames after the first, so two frames
	// of silence went unsent.
	packets[1].Timestamp = packets[0].Timestamp + 3*FrameSize

	if d := decodeAll(t, packets)[1]; d.silence != 2*FrameSize {
		t.Fatalf("silence = %d samples, want %d", d.silence, 2*FrameSize)
	}
}

func TestDecodeIgnoresBogusGaps(t *testing.T) {
	packets := tonePackets(t, 2)
	packets[1].Timestamp = packets[0].Timestamp + maxSilenceFill + FrameSize

	if d := decodeAll(t, packets)[1]; d.silence != 0 {
		t.Fatalf("silence = %d: a gap past maxSilenceFill is not a pause", d.silence)
	}
}

func TestDecodeFirstPacketHasNoGap(t *testing.T) {
	packets := tonePackets(t, 1)
	packets[0].Timestamp = 123456789

	if d := decodeAll(t, packets)[0]; d.silence != 0 {
		t.Fatalf("silence = %d: nothing precedes the first packet", d.silence)
	}
}

func TestDecodeSurvivesTimestampWrap(t *testing.T) {
	packets := tonePackets(t, 2)
	packets[0].Timestamp = math.MaxUint32 - 100
	packets[1].Timestamp = packets[0].Timestamp + FrameSize // wraps

	if d := decodeAll(t, packets)[1]; d.silence != 0 {
		t.Fatalf("silence = %d: a wrapped clock is still consecutive", d.silence)
	}
}
