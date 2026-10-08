package audio

import (
	"bytes"
	"encoding/binary"
	"math"
)

func dBToGain(dB float32) float32 {
	return float32(math.Pow(10.0, float64(dB/20)))
}

func clampFloat(clamp float32, min float32, max float32) float32 {
	if clamp < min {
		return min
	}

	if clamp > max {
		return max
	}

	return clamp
}

func normalizeInt(val int16) float32 {
	return float32(val) / 32767.0
}

func shrinkFloat(val float32) int16 {
	return int16(math.Round(float64(clampFloat(val, -1.0, 1.0)) * 32767.0))
}

func normalizeInts(arr []int16) []float32 {
	out := make([]float32, len(arr))
	for i, val := range arr {
		out[i] = normalizeInt(val)
	}

	return out
}

func shrinkFloats(arr []float32) []int16 {
	out := make([]int16, len(arr))
	for i, val := range arr {
		out[i] = shrinkFloat(val)
	}

	return out
}

// ByteToInt16 decodes little-endian s16le PCM bytes into samples.
func ByteToInt16(arr []byte) []int16 {
	out := make([]int16, len(arr)/2)
	binary.Read(bytes.NewReader(arr), binary.LittleEndian, out)
	return out
}

func int16ToByte(arr []int16) []byte {
	out := make([]byte, 0, len(arr)*2)
	for _, val := range arr {
		out = binary.LittleEndian.AppendUint16(out, uint16(val))
	}

	return out
}

func mixFloats(one []float32, two []float32) []float32 {
	len1 := len(one)
	len2 := len(two)

	var arrLength int
	if len1 < len2 {
		arrLength = len2
	} else {
		arrLength = len1
	}

	out := make([]float32, arrLength)
	for i := 0; i < arrLength; i++ {
		var first float32
		if i >= len1 {
			first = 0
		} else {
			first = one[i]
		}

		var second float32
		if i >= len2 {
			second = 0
		} else {
			second = two[i]
		}

		out[i] = first + second
	}

	return out
}
