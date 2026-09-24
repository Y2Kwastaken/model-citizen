package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	discordbot "github.com/Y2Kwastaken/model-citizen/discord-bot"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/audio"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/config"
	"github.com/Y2Kwastaken/model-citizen/llm"
)

const (
	MODEL_CONFIG_KEY = "MODEL_CONFIG"
	DISCORD_KEY      = "DISCORD_KEY"

	// the personality and history settings; personality paths are relative to it
	defaultModelConfig = "config/model.json"
)

func main() {
	ctx := context.Background()

	settings, err := config.NewModelSettings(envOr(MODEL_CONFIG_KEY, defaultModelConfig))
	if err != nil {
		slog.Error("error while reading model config", slog.Any("err", err))
		os.Exit(1)
	}

	brain, err := llm.NewBrainLanguageModel(llm.Config{
		SystemPrompts:    settings.Pesrsonalities,
		SelectedPrompt:   settings.DefaultPersonality,
		HistorySize:      settings.HistorySize,
		Temperature:      settings.Temperature,
		TopP:             settings.TopP,
		TextModelsFile:   settings.TextModelsFile,
		VoiceModelsFile:  settings.STTModelsFiles,
		SpeechModelsFile: settings.TTSModelFiles,
	})
	if err != nil {
		slog.Error("error while starting llm connector", slog.Any("err", err))
		os.Exit(1)
	}

	wake, err := audio.LoadWakeWord(settings.WakeWordRuntime, settings.WakeWordDirectory, settings.WakeWordFile)
	if err != nil {
		slog.Error("wake word not found", slog.Any("err", err))
		os.Exit(1)
	}

	client, err := discordbot.Start(ctx, brain, DISCORD_KEY, discordbot.Listening{Wake: wake, Threshold: float32(settings.WakeWordThreshold)})

	if err != nil {
		slog.Error("error while starting discord bot", slog.Any("err", err))
		os.Exit(1)
	}
	defer client.Close(ctx)

	slog.Info("model-citizen is now running. Press CTRL-C to exit.")
	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-s
}

func envOr(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
