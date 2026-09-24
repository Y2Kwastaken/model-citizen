package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	discordbot "github.com/Y2Kwastaken/model-citizen/discord-bot"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/audio"
	"github.com/Y2Kwastaken/model-citizen/llm"
	"github.com/Y2Kwastaken/model-citizen/llm/model"
	"github.com/Y2Kwastaken/model-citizen/settings"
)

const (
	MODEL_CONFIG_KEY = "MODEL_CONFIG"
	DISCORD_KEY      = "DISCORD_KEY"

	MUSIC_CONFIG_KEY = "MUSIC_CONFIG"
	VOICE_CONFIG_KEY = "VOICE_CONFIG"

	// the personality and history settings; personality paths are relative to it
	defaultModelConfig = "config/model.json"
	defaultMusicConfig = "config/music.json"
	defaultVoiceConfig = "config/voice.json"
)

func main() {
	ctx := context.Background()

	modelSettings, err := settings.NewModelSettings(envOr(MODEL_CONFIG_KEY, defaultModelConfig))
	if err != nil {
		slog.Error("error while reading model config", slog.Any("err", err))
		os.Exit(1)
	}

	musicSettings, err := settings.NewMusicSettings(envOr(MUSIC_CONFIG_KEY, defaultMusicConfig))
	if err != nil {
		slog.Error("error while reading music config", slog.Any("err", err))
		os.Exit(1)
	}

	voiceSettings, err := settings.NewVoiceSettings(envOr(VOICE_CONFIG_KEY, defaultVoiceConfig))
	if err != nil {
		slog.Error("error while reading voice config", slog.Any("err", err))
		os.Exit(1)
	}

	brain, err := llm.NewBrainLanguageModel(llm.Config{
		SystemPrompts:    modelSettings.Pesrsonalities,
		SelectedPrompt:   modelSettings.DefaultPersonality,
		HistorySize:      modelSettings.HistorySize,
		Temperature:      modelSettings.Temperature,
		TopP:             modelSettings.TopP,
		TextModelsFile:   modelSettings.TextModelsFile,
		VoiceModelsFile:  modelSettings.STTModelsFiles,
		SpeechModelsFile: modelSettings.TTSModelFiles,

		MaxReplyRunes:       modelSettings.MaxReplyRunes,
		MaxReplyTokens:      modelSettings.MaxReplyTokens,
		ToolRoundBonus:      modelSettings.ToolRoundBonus,
		MaxRedraws:          modelSettings.MaxRedraws,
		MaxToolRounds:       modelSettings.MaxToolRounds,
		ShortTermMemorySize: modelSettings.ShortTermMemorySize,
		PerModelTimeout:     modelSettings.PerModelTimeout,
		Rotation: model.Rotation{
			Reward: modelSettings.Rotation.Reward,
			Punish: modelSettings.Rotation.Punish,
			Kill:   modelSettings.Rotation.Kill,
			Parole: modelSettings.Rotation.Parole,
		},
	})
	if err != nil {
		slog.Error("error while starting llm connector", slog.Any("err", err))
		os.Exit(1)
	}

	wake, err := audio.LoadWakeWord(modelSettings.WakeWordRuntime, modelSettings.WakeWordDirectory, modelSettings.WakeWordFile)
	if err != nil {
		slog.Error("wake word not found", slog.Any("err", err))
		os.Exit(1)
	}

	client, err := discordbot.Start(ctx, brain, DISCORD_KEY, modelSettings.ReplyTimeout, discordbot.Listening{
		Wake:              wake,
		Threshold:         float32(modelSettings.WakeWordThreshold),
		ListenWindow:      voiceSettings.ListenWindow,
		ContextWindow:     voiceSettings.ContextWindow,
		CommandQuiet:      voiceSettings.CommandQuiet,
		CommandMax:        voiceSettings.CommandMax,
		MinUtterance:      voiceSettings.MinUtterance,
		WakeDebounce:      voiceSettings.WakeDebounce,
		Language:          voiceSettings.Language,
		TranscribeTimeout: voiceSettings.TranscribeTimeout,
		SpeakTimeout:      voiceSettings.SpeakTimeout,
	}, discordbot.Music{
		MaxQueueLength:  musicSettings.MaxQueueLength,
		CacheRetention:  musicSettings.CacheRetention,
		PrefetchAhead:   musicSettings.PrefetchAhead,
		PlaybackTimeout: musicSettings.PlaybackTimeout,
		Volume:          musicSettings.Volume,
		DuckedGain:      musicSettings.DuckedGain,
	})

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
