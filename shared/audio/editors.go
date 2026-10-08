package audio

type SampleEditor func(float32) float32

func ComposeFrameEditors(e1 SampleEditor, e2 SampleEditor) SampleEditor {
	return func(f float32) float32 {
		return e2(e1(f))
	}
}

func GainEditor(db float32) SampleEditor {
	gain := dBToGain(db)
	return func(f float32) float32 {
		return f * gain
	}
}

func RawGainEditor(gain float32) SampleEditor {
	return func(f float32) float32 {
		return f * gain
	}
}
