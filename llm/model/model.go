package model

import "context"

type ModelFeature int

const (
	Chat ModelFeature = iota
	STT
	TTS
)

type LanguageModel interface {
	HasFeature(feature ModelFeature) bool
	Tools() ModelTools
	History() HistoryProvider

	// functions
	Chat(ctx context.Context, origin Origin) (string, error)
	Transcribe(ctx context.Context, clip Clip) (string, error)
	Speak(ctx context.Context, text string) (Clip, error)
}
