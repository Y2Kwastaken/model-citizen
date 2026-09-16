package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	discordbot "github.com/Y2Kwastaken/model-citizen/discord-bot"
	"github.com/Y2Kwastaken/model-citizen/llm"
)

const (
	MODEL_FILE_KEY       = "MODEL_FILE"
	VOICE_MODEL_FILE_KEY = "VOICE_MODEL_FILE"
	MODEL_AUTH_KEY       = "MODEL_AUTH_KEY"
	MODEL_NAME_KEY       = "MODEL_NAME"
	MODEL_LINK_KEY       = "MODEL_LINK"
	DISCORD_KEY          = "DISCORD_KEY"

	// where the rotations are read from when the *_FILE variables are unset;
	// the compose file mounts data/*-models.json here
	defaultModelFile      = "text-models.json"
	defaultVoiceModelFile = "voice-models.json"
)

func main() {
	ctx := context.Background()

	brain, err := llm.NewBrainLanguageModel(llm.Config{
		TextModelsFile:  envOr(MODEL_FILE_KEY, defaultModelFile),
		VoiceModelsFile: envOr(VOICE_MODEL_FILE_KEY, defaultVoiceModelFile),
		FallbackAuthKey: MODEL_AUTH_KEY,
		FallbackNameKey: MODEL_NAME_KEY,
		FallbackLinkKey: MODEL_LINK_KEY,
	})
	if err != nil {
		slog.Error("error while starting llm connector", slog.Any("err", err))
		os.Exit(1)
	}

	client, err := discordbot.Start(ctx, brain, DISCORD_KEY)

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
