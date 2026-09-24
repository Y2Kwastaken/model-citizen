package settings

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

type ModelSettings struct {
	DefaultPersonality string
	Pesrsonalities     map[string]string
	TextModelsFile     string
	TTSModelFiles      string
	STTModelsFiles     string
	HistorySize        int
	Temperature        float64
	TopP               float64
	WakeWordThreshold  float64
	WakeWordDirectory  string
	WakeWordFile       string
	WakeWordRuntime    string

	MaxReplyRunes       int
	MaxReplyTokens      int
	ToolRoundBonus      int
	MaxRedraws          int
	MaxToolRounds       int
	ShortTermMemorySize int
	ReplyTimeout        time.Duration
	PerModelTimeout     time.Duration
	Rotation            RotationSettings
}

// RotationSettings is how model latency is judged and how long a failed model sits out.
type RotationSettings struct {
	Reward time.Duration
	Punish time.Duration
	Kill   time.Duration
	Parole time.Duration
}

type rotationSettingsRaw struct {
	RewardSeconds int `json:"reward_seconds"`
	PunishSeconds int `json:"punish_seconds"`
	KillSeconds   int `json:"kill_seconds"`
	ParoleSeconds int `json:"parole_seconds"`
}

type modelSettingsRaw struct {
	DefaultPersonality string            `json:"default_personality"`
	Personalities      map[string]string `json:"personalities"`
	TextModelsFile     string            `json:"text-models"`
	TTSModelFiles      string            `json:"tts-models"`
	STTModelsFiles     string            `json:"stt-models"`
	HistorySize        int               `json:"history_size"`
	Temperature        float64           `json:"temperature"`
	TopP               float64           `json:"top_p"`
	WakeWordThreshold  float64           `json:"wake_word_threshold"`
	WakeWordDirectory  string            `json:"wake_word_directory"`
	WakeWordFile       string            `json:"wake_word_file"`
	WakeWordRuntime    string            `json:"wake_word_runtime"`

	MaxReplyRunes          int                 `json:"max_reply_runes"`
	MaxReplyTokens         int                 `json:"max_reply_tokens"`
	ToolRoundBonus         int                 `json:"tool_round_bonus"`
	MaxRedraws             int                 `json:"max_redraws"`
	MaxToolRounds          int                 `json:"max_tool_rounds"`
	ShortTermMemorySize    int                 `json:"short_term_memory_size"`
	ReplyTimeoutSeconds    int                 `json:"reply_timeout_seconds"`
	PerModelTimeoutSeconds int                 `json:"per_model_timeout_seconds"`
	Rotation               rotationSettingsRaw `json:"rotation"`
}

func NewModelSettings(config string) (ModelSettings, error) {
	data, err := readRawModelSettings(config)
	if err != nil {
		return ModelSettings{}, nil
	}

	// file paths in the config are relative to the config file
	dir := filepath.Dir(config)

	personalities := make(map[string]string, len(data.Personalities))
	for name, personalityFile := range data.Personalities {
		personalityText, err := readFile(filepath.Join(dir, personalityFile))
		if err != nil {
			slog.Error("failed to read personality replacing with NOP", slog.String("name", name), slog.String("file", personalityFile), slog.Any("error", err))
			personalityText = "NOP"
		}
		personalities[name] = personalityText
	}

	if _, ok := personalities[data.DefaultPersonality]; !ok {
		slog.Error("failed to find default personality recovering and setting to NOP", slog.String("personality", data.DefaultPersonality))
		personalities[data.DefaultPersonality] = "NOP"
	}

	if data.Temperature < 0 || data.Temperature > 2 {
		return ModelSettings{}, fmt.Errorf("temperature must be between 0 and 2, got %v", data.Temperature)
	}
	if data.TopP <= 0 || data.TopP > 1 {
		return ModelSettings{}, fmt.Errorf("top_p must be above 0 and at most 1, got %v", data.TopP)
	}

	if data.MaxReplyRunes <= 0 || data.MaxReplyTokens <= 0 || data.ShortTermMemorySize <= 0 {
		return ModelSettings{}, fmt.Errorf("max_reply_runes, max_reply_tokens and short_term_memory_size must be above 0")
	}
	if data.ToolRoundBonus < 0 || data.MaxRedraws < 0 || data.MaxToolRounds < 0 {
		return ModelSettings{}, fmt.Errorf("tool_round_bonus, max_redraws and max_tool_rounds must be at least 0")
	}
	if data.ReplyTimeoutSeconds <= 0 || data.PerModelTimeoutSeconds <= 0 {
		return ModelSettings{}, fmt.Errorf("reply_timeout_seconds and per_model_timeout_seconds must be above 0")
	}
	rotation := data.Rotation
	if rotation.RewardSeconds <= 0 || rotation.RewardSeconds > rotation.PunishSeconds || rotation.PunishSeconds > rotation.KillSeconds {
		return ModelSettings{}, fmt.Errorf("rotation needs 0 < reward_seconds <= punish_seconds <= kill_seconds, got %d, %d, %d",
			rotation.RewardSeconds, rotation.PunishSeconds, rotation.KillSeconds)
	}
	if rotation.ParoleSeconds <= 0 {
		return ModelSettings{}, fmt.Errorf("rotation parole_seconds must be above 0, got %d", rotation.ParoleSeconds)
	}

	wakeWordThreshhold := data.WakeWordThreshold
	if wakeWordThreshhold < 0.1 {
		slog.Warn("threshold has minimum value of 0.1 setting to 0.1 from", slog.Float64("threshold", wakeWordThreshhold))
		wakeWordThreshhold = 0.1
	}

	return ModelSettings{
		DefaultPersonality: data.DefaultPersonality,
		Pesrsonalities:     personalities,
		TextModelsFile:     filepath.Join(dir, data.TextModelsFile),
		TTSModelFiles:      filepath.Join(dir, data.TTSModelFiles),
		STTModelsFiles:     filepath.Join(dir, data.STTModelsFiles),
		HistorySize:        data.HistorySize,
		Temperature:        data.Temperature,
		TopP:               data.TopP,
		WakeWordThreshold:  wakeWordThreshhold,
		WakeWordDirectory:  data.WakeWordDirectory,
		WakeWordFile:       data.WakeWordFile,
		WakeWordRuntime:    data.WakeWordRuntime,

		MaxReplyRunes:       data.MaxReplyRunes,
		MaxReplyTokens:      data.MaxReplyTokens,
		ToolRoundBonus:      data.ToolRoundBonus,
		MaxRedraws:          data.MaxRedraws,
		MaxToolRounds:       data.MaxToolRounds,
		ShortTermMemorySize: data.ShortTermMemorySize,
		ReplyTimeout:        seconds(data.ReplyTimeoutSeconds),
		PerModelTimeout:     seconds(data.PerModelTimeoutSeconds),
		Rotation: RotationSettings{
			Reward: seconds(rotation.RewardSeconds),
			Punish: seconds(rotation.PunishSeconds),
			Kill:   seconds(rotation.KillSeconds),
			Parole: seconds(rotation.ParoleSeconds),
		},
	}, nil
}

func readRawModelSettings(config string) (modelSettingsRaw, error) {
	data, err := os.ReadFile(config)
	if err != nil {
		return modelSettingsRaw{}, err
	}

	var settings modelSettingsRaw
	if err = json.Unmarshal(data, &settings); err != nil {
		return modelSettingsRaw{}, err
	}

	return settings, nil
}

func seconds(n int) time.Duration {
	return time.Duration(n) * time.Second
}

func readFile(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	return string(data), nil
}
