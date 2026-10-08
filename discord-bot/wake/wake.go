// claude authored, kept separate from the hand written audio code

package wake

import (
	"fmt"
	"path/filepath"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// openWakeWord's front end, fixed by its frozen models. audio moves in 80 ms
// steps, each step adds 8 mel frames and 1 embedding, and the score looks at
// the last 16 embeddings (1.28 s)
const (
	SampleRate    = 16000
	step          = SampleRate * 80 / 1000 // samples per step
	melHop        = 160
	melLookback   = 3 * melHop // samples before a step the mel model also needs
	melBins       = 32
	melsPerStep   = step / melHop
	melWindow     = 76 // mel frames per embedding
	embeddingSize = 96
	scoreWindow   = 16 // embeddings per score
)

// onnxruntime is set up once per process
var setup struct {
	sync.Once
	err error
}

// the three models, shared by every detector
type Model struct {
	// sessions reuse their tensors so only one inference runs at a time
	lock      sync.Mutex
	mel       *session
	embedding *session
	head      *session
}

type session struct {
	run *ort.DynamicAdvancedSession
	in  *ort.Tensor[float32]
	out *ort.Tensor[float32]
}

// loads the onnxruntime library then the two front end models and the wake
// word head from directory
func Load(library string, directory string, head string) (*Model, error) {
	setup.Do(func() {
		ort.SetSharedLibraryPath(library)
		setup.err = ort.InitializeEnvironment()
	})
	if setup.err != nil {
		return nil, fmt.Errorf("onnxruntime: %w", setup.err)
	}

	opts, err := sessionOptions()
	if err != nil {
		return nil, err
	}
	defer opts.Destroy()

	m := &Model{}
	m.mel, err = newSession(filepath.Join(directory, "melspectrogram.onnx"), opts,
		ort.NewShape(1, melLookback+step), ort.NewShape(1, 1, melsPerStep, melBins))
	if err != nil {
		return nil, err
	}

	m.embedding, err = newSession(filepath.Join(directory, "embedding_model.onnx"), opts,
		ort.NewShape(1, melWindow, melBins, 1), ort.NewShape(1, 1, 1, embeddingSize))
	if err != nil {
		return nil, err
	}

	m.head, err = newSession(filepath.Join(directory, head), opts,
		ort.NewShape(1, scoreWindow, embeddingSize), ort.NewShape(1, 1))
	if err != nil {
		return nil, err
	}

	return m, nil
}

// one thread per model and no spinning, the default thread pool burns most of
// a core waiting between 2 ms runs
func sessionOptions() (*ort.SessionOptions, error) {
	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, err
	}

	if err := opts.SetIntraOpNumThreads(1); err != nil {
		opts.Destroy()
		return nil, err
	}
	if err := opts.SetInterOpNumThreads(1); err != nil {
		opts.Destroy()
		return nil, err
	}
	if err := opts.AddSessionConfigEntry("session.intra_op.allow_spinning", "0"); err != nil {
		opts.Destroy()
		return nil, err
	}
	if err := opts.AddSessionConfigEntry("session.inter_op.allow_spinning", "0"); err != nil {
		opts.Destroy()
		return nil, err
	}

	return opts, nil
}

func newSession(path string, opts *ort.SessionOptions, in ort.Shape, out ort.Shape) (*session, error) {
	inputs, outputs, err := ort.GetInputOutputInfo(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(inputs) != 1 || len(outputs) != 1 {
		return nil, fmt.Errorf("%s: expected one input and one output", path)
	}

	inTensor, err := ort.NewEmptyTensor[float32](in)
	if err != nil {
		return nil, err
	}
	outTensor, err := ort.NewEmptyTensor[float32](out)
	if err != nil {
		return nil, err
	}

	run, err := ort.NewDynamicAdvancedSession(path, []string{inputs[0].Name}, []string{outputs[0].Name}, opts)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	return &session{run: run, in: inTensor, out: outTensor}, nil
}

// the caller must hold the model's lock
func (s *session) infer(input []float32) ([]float32, error) {
	copy(s.in.GetData(), input)
	if err := s.run.Run([]ort.Value{s.in}, []ort.Value{s.out}); err != nil {
		return nil, err
	}
	return s.out.GetData(), nil
}

// one speaker's detector, feed it 16 kHz mono and it scores every 80 ms
type Detector struct {
	model *Model
	// samples short of a full step
	pending []int16
	// the last melLookback samples of the previous step
	lookback []int16
	// newest last, only the last melWindow are kept
	mels [][]float32
	// newest last, only the last scoreWindow are kept
	embeddings [][]float32
	score      float32
}

func (m *Model) NewDetector() *Detector {
	d := &Detector{
		model:    m,
		lookback: make([]int16, melLookback),
	}

	// openWakeWord starts its mel frames at ones, matching it keeps scores the same
	for range melWindow {
		frame := make([]float32, melBins)
		for i := range frame {
			frame[i] = 1
		}
		d.mels = append(d.mels, frame)
	}

	return d
}

// takes any amount of audio and returns the score of the newest full step,
// 0 until scoreWindow steps have been heard
func (d *Detector) Feed(pcm []int16) (float32, error) {
	d.pending = append(d.pending, pcm...)
	for len(d.pending) >= step {
		if err := d.step(d.pending[:step]); err != nil {
			return 0, err
		}
		d.pending = d.pending[step:]
	}

	return d.score, nil
}

func (d *Detector) step(chunk []int16) error {
	input := make([]float32, 0, melLookback+step)
	for _, sample := range d.lookback {
		input = append(input, float32(sample))
	}
	for _, sample := range chunk {
		input = append(input, float32(sample))
	}
	copy(d.lookback, chunk[step-melLookback:])

	d.model.lock.Lock()
	defer d.model.lock.Unlock()

	// audio to mel frames
	mel, err := d.model.mel.infer(input)
	if err != nil {
		return fmt.Errorf("melspectrogram: %w", err)
	}
	for f := range melsPerStep {
		frame := make([]float32, melBins)
		for b := range frame {
			// openWakeWord's rescale to match the model it was trained against
			frame[b] = mel[f*melBins+b]/10 + 2
		}
		d.mels = append(d.mels, frame)
	}
	d.mels = d.mels[len(d.mels)-melWindow:]

	// the last melWindow mel frames to one embedding
	window := make([]float32, 0, melWindow*melBins)
	for _, frame := range d.mels {
		window = append(window, frame...)
	}
	embedding, err := d.model.embedding.infer(window)
	if err != nil {
		return fmt.Errorf("embedding: %w", err)
	}
	d.embeddings = append(d.embeddings, append([]float32(nil), embedding...))
	if len(d.embeddings) < scoreWindow {
		return nil
	}
	d.embeddings = d.embeddings[len(d.embeddings)-scoreWindow:]

	// the last scoreWindow embeddings to a score
	features := make([]float32, 0, scoreWindow*embeddingSize)
	for _, embedding := range d.embeddings {
		features = append(features, embedding...)
	}
	score, err := d.model.head.infer(features)
	if err != nil {
		return fmt.Errorf("head: %w", err)
	}
	d.score = score[0]

	return nil
}
