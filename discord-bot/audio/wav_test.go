// claude authored, kept separate from the hand written audio code

package audio

import (
	"encoding/binary"
	"testing"
)

func TestEncodeWAV(t *testing.T) {
	pcm := []int16{0, 1, -1, 32767, -32768}
	wav := EncodeWAV(pcm)

	if len(wav) != 44+len(pcm)*2 {
		t.Fatalf("wav is %d bytes, want %d", len(wav), 44+len(pcm)*2)
	}
	if string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" || string(wav[12:16]) != "fmt " || string(wav[36:40]) != "data" {
		t.Fatal("chunk ids are wrong")
	}
	if rate := binary.LittleEndian.Uint32(wav[24:]); rate != 16000 {
		t.Fatalf("sample rate %d, want 16000", rate)
	}
	if channels := binary.LittleEndian.Uint16(wav[22:]); channels != 1 {
		t.Fatalf("%d channels, want 1", channels)
	}
	for i, want := range pcm {
		if got := int16(binary.LittleEndian.Uint16(wav[44+i*2:])); got != want {
			t.Fatalf("sample %d is %d, want %d", i, got, want)
		}
	}
}
