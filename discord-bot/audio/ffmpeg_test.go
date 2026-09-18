package audio

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestEncodeWAVIsReadableAndResampled(t *testing.T) {
	// a second of tone, mono at SampleRate
	pcm := make([]int16, SampleRate)
	for i := range pcm {
		pcm[i] = int16(8000 * math.Sin(2*math.Pi*440*float64(i)/SampleRate))
	}
	path := filepath.Join(t.TempDir(), "tone.wav")
	if err := os.WriteFile(path, EncodeWAV(pcm), 0o644); err != nil {
		t.Fatal(err)
	}
	back := readWAV(t, path)
	if len(back) != WakeRate {
		t.Fatalf("%d samples back, want %d: a second at 16kHz", len(back), WakeRate)
	}
	var peak int16
	for _, s := range back[100:] {
		peak = max(peak, s)
	}
	if peak < 7500 {
		t.Fatalf("tone peaked at %d after the round trip, want ~8000", peak)
	}
}
