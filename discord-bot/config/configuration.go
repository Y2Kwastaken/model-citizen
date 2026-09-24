package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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

func readFile(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	return string(data), nil
}
