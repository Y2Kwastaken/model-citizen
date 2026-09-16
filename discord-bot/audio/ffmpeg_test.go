package audio

import (
	"bytes"
	"math"
	"os/exec"
	"testing"
)

func TestEncodeFLACProducesAFile(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}

	// a second of tone, mono at SampleRate
	pcm := make([]int16, SampleRate)
	for i := range pcm {
		pcm[i] = int16(8000 * math.Sin(2*math.Pi*440*float64(i)/SampleRate))
	}

	flac, err := EncodeFLAC(pcm, 16000)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(flac, []byte("fLaC")) {
		t.Fatalf("output does not start with the flac marker: % x", flac[:min(8, len(flac))])
	}
	// 16kHz mono s16 is 32kB/s raw; a tone compresses well below that
	if len(flac) == 0 || len(flac) > 2*SampleRate {
		t.Fatalf("flac is %d bytes, want something between a header and the raw size", len(flac))
	}
}
