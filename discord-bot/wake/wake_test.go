// claude authored, kept separate from the hand written audio code

package wake

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"testing"
)

const (
	modelDirectory = "../assets/wake"
	// the shipped wake word
	shipped = "hey_model.onnx"
	// the version the python reference scores were recorded with, relative to modelDirectory
	referenceHead = "../../wake/testdata/hey_model_v6.onnx"
)

// needs libonnxruntime.so 1.29.x from ONNXRUNTIME_LIB, skipped without it
func loadModel(t *testing.T, head string) *Model {
	t.Helper()
	library := os.Getenv("ONNXRUNTIME_LIB")
	if library == "" {
		t.Skip("set ONNXRUNTIME_LIB to run the wake word tests")
	}

	m, err := Load(library, modelDirectory, head)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// reads 16 bit mono pcm from a wav, skipping any chunks before the data
func readWAV(t *testing.T, path string) []int16 {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	for at := 12; at+8 <= len(raw); {
		id := string(raw[at : at+4])
		size := int(binary.LittleEndian.Uint32(raw[at+4:]))
		if id == "data" {
			body := raw[at+8 : at+8+size]
			pcm := make([]int16, len(body)/2)
			for i := range pcm {
				pcm[i] = int16(binary.LittleEndian.Uint16(body[i*2:]))
			}
			return pcm
		}
		at += 8 + size + size%2
	}

	t.Fatalf("%s has no data chunk", path)
	return nil
}

// scores of every full step, fed one step at a time
func scores(t *testing.T, m *Model, pcm []int16) []float32 {
	t.Helper()
	d := m.NewDetector()
	var out []float32
	for i := 0; i+step <= len(pcm); i += step {
		score, err := d.Feed(pcm[i : i+step])
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, score)
	}
	return out
}

func peak(scores []float32) float32 {
	var best float32
	for _, score := range scores {
		best = max(best, score)
	}
	return best
}

// the same clips through openWakeWord's python must give the same scores.
// python fills its first embeddings with noise, so only scores after the
// window is full of real audio are compared
func TestMatchesPython(t *testing.T) {
	m := loadModel(t, referenceHead)

	raw, err := os.ReadFile("testdata/hey_model_scores.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference map[string]struct {
		Scores []float32 `json:"scores"`
	}
	if err := json.Unmarshal(raw, &reference); err != nil {
		t.Fatal(err)
	}

	for name, ref := range reference {
		got := scores(t, m, readWAV(t, "testdata/hey_model_"+name+".wav"))
		if len(got) != len(ref.Scores) {
			t.Fatalf("%s: %d scores, python had %d", name, len(got), len(ref.Scores))
		}
		// onnxruntime's float ordering moves the steepest edge by about 0.02
		for i := scoreWindow; i < len(got); i++ {
			if diff := math.Abs(float64(got[i] - ref.Scores[i])); diff > 0.03 {
				t.Errorf("%s step %d: go %.4f python %.4f", name, i, got[i], ref.Scores[i])
			}
		}
	}
}

func TestHearsThePhrase(t *testing.T) {
	m := loadModel(t, shipped)

	if best := peak(scores(t, m, readWAV(t, "testdata/hey_model_positive.wav"))); best < 0.5 {
		t.Errorf("'hey model' peaked at %.3f, want over 0.5", best)
	}
	if best := peak(scores(t, m, readWAV(t, "testdata/hey_model_negative.wav"))); best > 0.2 {
		t.Errorf("the negative clip peaked at %.3f, want under 0.2", best)
	}
	// a real voice with a real pause between the words
	for _, clip := range []string{"miles_hey_model_1", "miles_hey_model_2"} {
		if best := peak(scores(t, m, readWAV(t, "testdata/"+clip+".wav"))); best < 0.5 {
			t.Errorf("%s peaked at %.3f, want over 0.5", clip, best)
		}
	}
}

// discord delivers 20 ms at a time, 320 samples after downsampling, which
// must score exactly like whole steps
func TestFeedsAnySize(t *testing.T) {
	m := loadModel(t, shipped)
	pcm := readWAV(t, "testdata/hey_model_positive.wav")
	whole := scores(t, m, pcm)

	d := m.NewDetector()
	var pieces []float32
	for i := 0; i+320 <= len(pcm); i += 320 {
		score, err := d.Feed(pcm[i : i+320])
		if err != nil {
			t.Fatal(err)
		}
		if (i+320)%step == 0 {
			pieces = append(pieces, score)
		}
	}

	if len(pieces) != len(whole) {
		t.Fatalf("%d scores in pieces, %d whole", len(pieces), len(whole))
	}
	for i := range whole {
		if pieces[i] != whole[i] {
			t.Fatalf("step %d: pieces %.4f whole %.4f", i, pieces[i], whole[i])
		}
	}
}
