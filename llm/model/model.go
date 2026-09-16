package model

import "context"

type ModelFeature int

const (
	Chat ModelFeature = iota
	SpeechToText
)

type LanguageModel interface {
	HasFeature(feature ModelFeature) bool
	Tools() ModelTools
	History() HistoryProvider

	// functions
	Chat(ctx context.Context, origin Origin) (string, error)
}
