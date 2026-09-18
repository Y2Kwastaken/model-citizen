package audio

import "math"

// WakeRate is what the wake word models hear.
const WakeRate = 16000

const decimation = SampleRate / WakeRate

// The anti-alias filter: a windowed sinc low-pass at 48kHz. Cutoff sits
// just under the 8kHz that survives decimation, so what folds down is
// attenuated rather than landing in the speech band. The detector was
// trained on properly resampled audio; a plain average of three samples
// aliases everything above 8kHz into what it hears, and that is not speech
// it has ever seen.
const (
	firTaps   = 63
	firCutoff = 7200.0 / SampleRate
)

var firKernel = func() []float64 {
	kernel := make([]float64, firTaps)
	mid := (firTaps - 1) / 2
	var sum float64
	for i := range kernel {
		n := float64(i - mid)
		sinc := 2 * firCutoff
		if n != 0 {
			sinc = math.Sin(2*math.Pi*firCutoff*n) / (math.Pi * n)
		}
		hamming := 0.54 - 0.46*math.Cos(2*math.Pi*float64(i)/float64(firTaps-1))
		kernel[i] = sinc * hamming
		sum += kernel[i]
	}
	for i := range kernel {
		kernel[i] /= sum
	}
	return kernel
}()

// decimator takes SampleRate mono down to WakeRate: low-pass, then keep
// every third sample. It is streaming, carrying the filter's history and any
// samples short of a multiple of three across calls.
type decimator struct {
	history []int16 // the last firTaps-1 input samples
	phase   int     // samples into the current triple
}

func (d *decimator) Downsample(pcm []int16) []int16 {
	if d.history == nil {
		d.history = make([]int16, firTaps-1)
	}
	// filter over history + pcm without copying pcm
	buf := append(d.history, pcm...)
	out := make([]int16, 0, len(pcm)/decimation+1)
	for i := firTaps - 1; i < len(buf); i++ {
		if d.phase == 0 {
			var acc float64
			for k, w := range firKernel {
				acc += w * float64(buf[i-k])
			}
			out = append(out, int16(math.Max(-32768, math.Min(32767, math.Round(acc)))))
		}
		d.phase = (d.phase + 1) % decimation
	}
	d.history = append(d.history[:0], buf[len(buf)-(firTaps-1):]...)
	return out
}
