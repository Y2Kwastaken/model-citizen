package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type VoiceSettings struct {
	// empty lets the transcriber guess
	Language string
	// retained voice length per speaker
	ListenWindow time.Duration
	// how much before a wake goes with it, at most ListenWindow
	ContextWindow time.Duration
	// silence that ends a command
	CommandQuiet time.Duration
	// longest command listened to
	CommandMax time.Duration
	// shorter utterances are noise
	MinUtterance      time.Duration
	WakeDebounce      time.Duration
	TranscribeTimeout time.Duration
	// synthesis plus playback of one reply
	SpeakTimeout time.Duration
}

type voiceSettingsRaw struct {
	Language                 string `json:"language"`
	ListenWindowSeconds      int    `json:"listen_window_seconds"`
	ContextWindowSeconds     int    `json:"context_window_seconds"`
	CommandQuietMs           int    `json:"command_quiet_ms"`
	CommandMaxSeconds        int    `json:"command_max_seconds"`
	MinUtteranceMs           int    `json:"min_utterance_ms"`
	WakeDebounceSeconds      int    `json:"wake_debounce_seconds"`
	TranscribeTimeoutSeconds int    `json:"transcribe_timeout_seconds"`
	SpeakTimeoutSeconds      int    `json:"speak_timeout_seconds"`
}

func NewVoiceSettings(config string) (VoiceSettings, error) {
	data, err := os.ReadFile(config)
	if err != nil {
		return VoiceSettings{}, err
	}

	var raw voiceSettingsRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return VoiceSettings{}, err
	}

	positive := map[string]int{
		"listen_window_seconds":      raw.ListenWindowSeconds,
		"context_window_seconds":     raw.ContextWindowSeconds,
		"command_quiet_ms":           raw.CommandQuietMs,
		"command_max_seconds":        raw.CommandMaxSeconds,
		"min_utterance_ms":           raw.MinUtteranceMs,
		"transcribe_timeout_seconds": raw.TranscribeTimeoutSeconds,
		"speak_timeout_seconds":      raw.SpeakTimeoutSeconds,
	}
	for key, value := range positive {
		if value <= 0 {
			return VoiceSettings{}, fmt.Errorf("%s must be above 0, got %d", key, value)
		}
	}
	if raw.WakeDebounceSeconds < 0 {
		return VoiceSettings{}, fmt.Errorf("wake_debounce_seconds must be at least 0, got %d", raw.WakeDebounceSeconds)
	}
	if raw.ContextWindowSeconds > raw.ListenWindowSeconds {
		return VoiceSettings{}, fmt.Errorf("context_window_seconds (%d) cannot exceed listen_window_seconds (%d)",
			raw.ContextWindowSeconds, raw.ListenWindowSeconds)
	}

	return VoiceSettings{
		Language:          raw.Language,
		ListenWindow:      seconds(raw.ListenWindowSeconds),
		ContextWindow:     seconds(raw.ContextWindowSeconds),
		CommandQuiet:      time.Duration(raw.CommandQuietMs) * time.Millisecond,
		CommandMax:        seconds(raw.CommandMaxSeconds),
		MinUtterance:      time.Duration(raw.MinUtteranceMs) * time.Millisecond,
		WakeDebounce:      seconds(raw.WakeDebounceSeconds),
		TranscribeTimeout: seconds(raw.TranscribeTimeoutSeconds),
		SpeakTimeout:      seconds(raw.SpeakTimeoutSeconds),
	}, nil
}
