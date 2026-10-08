// claude authored, kept separate from the hand written audio code

package audio

import "math"

// 48 kHz mono down to 16 kHz for the wake word. a low pass first stops
// anything above 8 kHz folding down into the speech band, then every third
// sample is kept
const (
	downsampleFactor = 3
	lowPassTaps      = 63
	// just under the 8 kHz that 16 kHz can hold
	lowPassCutoff = 7200.0 / 48000
)

// a windowed sinc low pass, scaled so it doesn't change the volume
var lowPass = func() []float64 {
	taps := make([]float64, lowPassTaps)
	middle := (lowPassTaps - 1) / 2

	var sum float64
	for i := range taps {
		n := float64(i - middle)
		sinc := 2 * lowPassCutoff
		if n != 0 {
			sinc = math.Sin(2*math.Pi*lowPassCutoff*n) / (math.Pi * n)
		}
		hamming := 0.54 - 0.46*math.Cos(2*math.Pi*float64(i)/float64(lowPassTaps-1))
		taps[i] = sinc * hamming
		sum += taps[i]
	}

	for i := range taps {
		taps[i] /= sum
	}
	return taps
}()

// keeps the filter's history between calls so it can be fed a packet at a time
type Downsampler struct {
	// the last lowPassTaps-1 samples of the previous call
	history []int16
	// where the next sample sits in its group of three
	phase int
}

func NewDownsampler() *Downsampler {
	return &Downsampler{history: make([]int16, lowPassTaps-1)}
}

func (d *Downsampler) Downsample(pcm []int16) []int16 {
	samples := make([]int16, 0, len(d.history)+len(pcm))
	samples = append(samples, d.history...)
	samples = append(samples, pcm...)

	out := make([]int16, 0, len(pcm)/downsampleFactor+1)
	for i := len(d.history); i < len(samples); i++ {
		if d.phase == 0 {
			var filtered float64
			for k, tap := range lowPass {
				filtered += tap * float64(samples[i-k])
			}
			out = append(out, int16(math.Max(-32768, math.Min(32767, math.Round(filtered)))))
		}
		d.phase = (d.phase + 1) % downsampleFactor
	}

	copy(d.history, samples[len(samples)-len(d.history):])
	return out
}
