package audio

import (
	"fmt"
	"path/filepath"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// The openWakeWord front end, fixed by the frozen models it ships. Audio
// advances in 80ms chunks; each one yields 8 mel frames, one embedding, and
// one score over the last 1.28s.
const (
	wakeChunk        = WakeRate * 80 / 1000 // samples per step
	melHop           = 160                  // samples per mel frame
	melContext       = 3 * melHop           // the mel model wants this much before a chunk
	melBins          = 32
	melFramesPerStep = wakeChunk / melHop
	melWindow        = 76 // frames per embedding
	melHistory       = 10 * 97
	embeddingSize    = 96
	scoreWindow      = 16 // embeddings per score
	embeddingHistory = 120
)

// initOnce sets up the onnxruntime environment, which is per process.
var initOnce struct {
	sync.Once
	err error
}

// WakeWord holds the three models. Detectors share them; the mutex serialises
// runs because each session's tensors are allocated once, and inference is
// about a millisecond so nothing waits long.
type WakeWord struct {
	mu   sync.Mutex
	mel  *session
	emb  *session
	head *session
}

// session is one model with its fixed input and output.
type session struct {
	run *ort.DynamicAdvancedSession
	in  *ort.Tensor[float32]
	out *ort.Tensor[float32]
}

// LoadWakeWord loads the shared onnxruntime library and, from dir, the two
// front-end models plus the head named by model.
func LoadWakeWord(library string, dir string, model string) (*WakeWord, error) {
	initOnce.Do(func() {
		ort.SetSharedLibraryPath(library)
		initOnce.err = ort.InitializeEnvironment()
	})
	if initOnce.err != nil {
		return nil, fmt.Errorf("onnxruntime: %w", initOnce.err)
	}

	w := &WakeWord{}
	var err error
	if w.mel, err = newSession(filepath.Join(dir, "melspectrogram.onnx"),
		ort.NewShape(1, wakeChunk+melContext), ort.NewShape(1, 1, melFramesPerStep, melBins)); err != nil {
		return nil, err
	}
	if w.emb, err = newSession(filepath.Join(dir, "embedding_model.onnx"),
		ort.NewShape(1, melWindow, melBins, 1), ort.NewShape(1, 1, 1, embeddingSize)); err != nil {
		return nil, err
	}
	if w.head, err = newSession(filepath.Join(dir, model),
		ort.NewShape(1, scoreWindow, embeddingSize), ort.NewShape(1, 1)); err != nil {
		return nil, err
	}
	return w, nil
}

func newSession(path string, in ort.Shape, out ort.Shape) (*session, error) {
	inputs, outputs, err := ort.GetInputOutputInfo(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(inputs) != 1 || len(outputs) != 1 {
		return nil, fmt.Errorf("%s: want one input and one output, got %d and %d", path, len(inputs), len(outputs))
	}

	s := &session{}
	if s.in, err = ort.NewEmptyTensor[float32](in); err != nil {
		return nil, err
	}
	if s.out, err = ort.NewEmptyTensor[float32](out); err != nil {
		return nil, err
	}
	if s.run, err = ort.NewDynamicAdvancedSession(path, []string{inputs[0].Name}, []string{outputs[0].Name}, nil); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// infer copies input in, runs, and returns the output. The caller holds w.mu.
func (s *session) infer(input []float32) ([]float32, error) {
	copy(s.in.GetData(), input)
	if err := s.run.Run([]ort.Value{s.in}, []ort.Value{s.out}); err != nil {
		return nil, err
	}
	return s.out.GetData(), nil
}

// Detector is one speaker's streaming state. Feed it 16kHz mono and it
// scores every 80ms of it.
type Detector struct {
	w       *WakeWord
	pending []int16     // samples short of a chunk
	raw     []int16     // last melContext samples, for the mel model's lookback
	mel     [][]float32 // ring of mel frames, newest last
	emb     [][]float32 // ring of embeddings, newest last
	score   float32
}

func (w *WakeWord) NewDetector() *Detector {
	d := &Detector{w: w, raw: make([]int16, melContext)}
	// openWakeWord starts its mel buffer at ones, and the first few
	// embeddings look back into that. Matching it keeps scores comparable.
	for range melWindow {
		frame := make([]float32, melBins)
		for i := range frame {
			frame[i] = 1
		}
		d.mel = append(d.mel, frame)
	}
	return d
}

// Feed takes any amount of 16kHz mono and returns the score for the newest
// complete chunk. Until scoreWindow chunks have been heard the score is 0.
func (d *Detector) Feed(pcm []int16) (float32, error) {
	d.pending = append(d.pending, pcm...)
	for len(d.pending) >= wakeChunk {
		if err := d.step(d.pending[:wakeChunk]); err != nil {
			return 0, err
		}
		d.pending = d.pending[wakeChunk:]
	}
	return d.score, nil
}

// Score is the most recent score without feeding anything.
func (d *Detector) Score() float32 { return d.score }

// step advances the pipeline by one chunk.
func (d *Detector) step(chunk []int16) error {
	// mel input is the chunk with the samples before it
	input := make([]float32, 0, melContext+wakeChunk)
	for _, s := range d.raw {
		input = append(input, float32(s))
	}
	for _, s := range chunk {
		input = append(input, float32(s))
	}
	copy(d.raw, chunk[wakeChunk-melContext:])

	d.w.mu.Lock()
	defer d.w.mu.Unlock()

	mel, err := d.w.mel.infer(input)
	if err != nil {
		return fmt.Errorf("melspectrogram: %w", err)
	}
	for f := range melFramesPerStep {
		frame := make([]float32, melBins)
		for b := range frame {
			// openWakeWord's transform to approximate the original TF model
			frame[b] = mel[f*melBins+b]/10 + 2
		}
		d.mel = append(d.mel, frame)
	}
	if len(d.mel) > melHistory {
		d.mel = d.mel[len(d.mel)-melHistory:]
	}

	// one embedding over the last melWindow frames
	window := make([]float32, 0, melWindow*melBins)
	for _, frame := range d.mel[len(d.mel)-melWindow:] {
		window = append(window, frame...)
	}
	emb, err := d.w.emb.infer(window)
	if err != nil {
		return fmt.Errorf("embedding: %w", err)
	}
	d.emb = append(d.emb, append([]float32(nil), emb...))
	if len(d.emb) > embeddingHistory {
		d.emb = d.emb[len(d.emb)-embeddingHistory:]
	}
	if len(d.emb) < scoreWindow {
		return nil
	}

	features := make([]float32, 0, scoreWindow*embeddingSize)
	for _, e := range d.emb[len(d.emb)-scoreWindow:] {
		features = append(features, e...)
	}
	score, err := d.w.head.infer(features)
	if err != nil {
		return fmt.Errorf("head: %w", err)
	}
	d.score = score[0]
	return nil
}
