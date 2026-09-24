package model

import (
	"context"

	"github.com/disgoorg/snowflake/v2"
)

type ModelFeature int

const (
	Chat ModelFeature = iota
	STT
	TTS
)

type LanguageModel interface {
	HasFeature(feature ModelFeature) bool
	MemorySet() MemorySet
	Tools() ModelTools
	History() HistoryProvider

	// tweaks
	SetPersonality(guild snowflake.ID, name string) error

	// functions
	Chat(ctx context.Context, origin Origin) (string, error)
	Transcribe(ctx context.Context, clip Clip) (string, error)
	Speak(ctx context.Context, text string) (Clip, error)
}
