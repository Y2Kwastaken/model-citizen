package audio

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const wakeDir = "../../wakeword"

// onnxruntimeLibrary finds libonnxruntime.so: ONNXRUNTIME_LIB, else a release
// unpacked under wakeword/data. Tests that need it skip without one. The Go
// binding is built against one ORT API version, so it has to be the release
// the Dockerfile pins, not whatever the training venv happens to have.
func onnxruntimeLibrary(t *testing.T) string {
	t.Helper()
	if lib := os.Getenv("ONNXRUNTIME_LIB"); lib != "" {
		return lib
	}
	matches, _ := filepath.Glob(filepath.Join(wakeDir, "data/onnxruntime-linux-*/lib/libonnxruntime.so"))
	if len(matches) == 0 {
		t.Skip("no onnxruntime library: set ONNXRUNTIME_LIB")
	}
	return matches[0]
}

func loadWakeWord(t *testing.T) *WakeWord {
	t.Helper()
	w, err := LoadWakeWord(onnxruntimeLibrary(t), wakeDir, "hey_model.onnx")
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// readWAV reads 16-bit mono PCM from a wav, walking chunks so ffmpeg's
// LIST metadata does not get in the way.
func readWAV(t *testing.T, path string) []int16 {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 12 || string(raw[:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		t.Fatalf("%s: not a wav", path)
	}
	for at := 12; at+8 <= len(raw); {
		id, size := string(raw[at:at+4]), int(binary.LittleEndian.Uint32(raw[at+4:]))
		body := raw[at+8 : min(at+8+size, len(raw))]
		if id == "data" {
			pcm := make([]int16, len(body)/2)
			for i := range pcm {
				pcm[i] = int16(binary.LittleEndian.Uint16(body[i*2:]))
			}
			return pcm
		}
		at += 8 + size + size%2
	}
	t.Fatalf("%s: no data chunk", path)
	return nil
}

// reference is what openWakeWord's own Python implementation scored the
// fixture clips at, chunk by chunk (see wakeword/ for how it was produced).
func reference(t *testing.T) map[string]struct {
	Source string    `json:"source"`
	Scores []float32 `json:"scores"`
} {
	t.Helper()
	raw, err := os.ReadFile("testdata/hey_model_scores.json")
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]struct {
		Source string    `json:"source"`
		Scores []float32 `json:"scores"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func stream(t *testing.T, w *WakeWord, pcm []int16) []float32 {
	t.Helper()
	d := w.NewDetector()
	var scores []float32
	for i := 0; i+wakeChunk <= len(pcm); i += wakeChunk {
		score, err := d.Feed(pcm[i : i+wakeChunk])
		if err != nil {
			t.Fatal(err)
		}
		scores = append(scores, score)
	}
	return scores
}

// TestDetectorMatchesPython is the port's correctness check: the same audio
// through the same models must give the same numbers. Python seeds its
// embedding buffer with random noise, so only chunks once the window is
// full of real audio are comparable.
func TestDetectorMatchesPython(t *testing.T) {
	w := loadWakeWord(t)
	for name, ref := range reference(t) {
		got := stream(t, w, readWAV(t, "testdata/hey_model_"+name+".wav"))
		if len(got) != len(ref.Scores) {
			t.Fatalf("%s: %d chunks, python scored %d", name, len(got), len(ref.Scores))
		}
		// Agreement is to four decimals except on the steepest edge of the
		// score, where onnxruntime's own float ordering moves it by ~0.02.
		for i := scoreWindow; i < len(got); i++ {
			if diff := math.Abs(float64(got[i] - ref.Scores[i])); diff > 0.03 {
				t.Errorf("%s chunk %d: go=%.4f python=%.4f", name, i, got[i], ref.Scores[i])
			}
		}
	}
}

func TestDetectorHearsThePhrase(t *testing.T) {
	w := loadWakeWord(t)
	best := func(name string) float32 {
		var m float32
		for _, s := range stream(t, w, readWAV(t, "testdata/hey_model_"+name+".wav")) {
			m = max(m, s)
		}
		return m
	}
	if s := best("positive"); s < 0.5 {
		t.Errorf("'hey model' peaked at %.3f, want over 0.5", s)
	}
	if s := best("negative"); s > 0.2 {
		t.Errorf("the negative phrase peaked at %.3f, want under 0.2", s)
	}
}

// TestDetectorHearsMiles is the phrase as its owner actually says it: a low
// voice, and a real pause between the two words. Clip 2 has the longer
// pause; hey_model.onnx v2 scored it 0.001.
func TestDetectorHearsMiles(t *testing.T) {
	w := loadWakeWord(t)
	for _, clip := range []string{"miles_hey_model_1", "miles_hey_model_2"} {
		var peak float32
		for _, s := range stream(t, w, readWAV(t, "testdata/"+clip+".wav")) {
			peak = max(peak, s)
		}
		if peak < 0.5 {
			t.Errorf("%s peaked at %.3f, want over 0.5", clip, peak)
		}
	}
}

func TestDetectorFeedsAnyChunkSize(t *testing.T) {
	w := loadWakeWord(t)
	pcm := readWAV(t, "testdata/hey_model_positive.wav")
	whole := stream(t, w, pcm)

	// Discord delivers 20ms at a time after downsampling: 320 samples, a
	// quarter of a chunk. Scores must land on the same chunk boundaries.
	d := w.NewDetector()
	var piecemeal []float32
	last := float32(-1)
	for i := 0; i+320 <= len(pcm); i += 320 {
		score, err := d.Feed(pcm[i : i+320])
		if err != nil {
			t.Fatal(err)
		}
		if (i+320)%wakeChunk == 0 {
			piecemeal = append(piecemeal, score)
		} else if score != last && len(piecemeal) > 0 && score != piecemeal[len(piecemeal)-1] {
			t.Fatalf("score changed between chunk boundaries at sample %d", i)
		}
		last = score
	}
	if len(piecemeal) != len(whole) {
		t.Fatalf("%d scores piecemeal, %d whole", len(piecemeal), len(whole))
	}
	for i := range whole {
		if piecemeal[i] != whole[i] {
			t.Fatalf("chunk %d: piecemeal %.4f != whole %.4f", i, piecemeal[i], whole[i])
		}
	}
}

func TestDecimatorIsStreaming(t *testing.T) {
	// a 440Hz tone at 48kHz, fed whole and in Discord-sized pieces
	in := make([]int16, SampleRate)
	for i := range in {
		in[i] = int16(8000 * math.Sin(2*math.Pi*440*float64(i)/SampleRate))
	}
	var whole, pieces decimator
	all := whole.Downsample(in)
	var chunked []int16
	for i := 0; i < len(in); i += FrameSize {
		chunked = append(chunked, pieces.Downsample(in[i:i+FrameSize])...)
	}
	if len(all) != WakeRate || len(chunked) != len(all) {
		t.Fatalf("got %d whole and %d chunked samples, want %d", len(all), len(chunked), WakeRate)
	}
	for i := range all {
		if all[i] != chunked[i] {
			t.Fatalf("sample %d: whole %d != chunked %d", i, all[i], chunked[i])
		}
	}
	// past the filter's warm-up the tone comes through at full level
	var peak int16
	for _, s := range all[100:] {
		peak = max(peak, s)
	}
	if peak < 7500 {
		t.Fatalf("a 440Hz tone peaked at %d after decimation, want ~8000", peak)
	}
}

// TestDecimatorRejectsWhatWouldAlias is why the filter exists: content above
// 8kHz has to be attenuated, not folded into the band the detector hears.
func TestDecimatorRejectsWhatWouldAlias(t *testing.T) {
	in := make([]int16, SampleRate)
	for i := range in {
		in[i] = int16(8000 * math.Sin(2*math.Pi*13000*float64(i)/SampleRate)) // would alias to 3kHz
	}
	var d decimator
	out := d.Downsample(in)
	var peak int16
	for _, s := range out[100:] {
		peak = max(peak, s)
	}
	if peak > 400 { // -26dB or better
		t.Fatalf("a 13kHz tone came through at %d after decimation, want under 400", peak)
	}
}

// TestDetectorSurvivesHighBandNoise feeds the phrase with hiss above 8kHz
// on top, the way a real mic delivers it, through the full 48kHz path.
func TestDetectorSurvivesHighBandNoise(t *testing.T) {
	l, _ := newTestListener(loadWakeWord(t), 10*time.Second)
	pcm16 := readWAV(t, "testdata/hey_model_positive.wav")
	for _, p := range encodePacketsWithHiss(t, pcm16) {
		_ = l.ReceiveOpusFrame(7, p)
	}
	select {
	case <-l.Triggers():
	default:
		t.Fatal("the wake word was lost under high-band noise")
	}
}
