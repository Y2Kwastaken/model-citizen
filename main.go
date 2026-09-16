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
	MODEL_FILE_KEY = "MODEL_FILE"
	MODEL_AUTH_KEY = "MODEL_AUTH_KEY"
	MODEL_NAME_KEY = "MODEL_NAME"
	MODEL_LINK_KEY = "MODEL_LINK"
	DISCORD_KEY    = "DISCORD_KEY"

	// where the rotation is read from when MODEL_FILE is unset; the compose
	// file mounts data/models.json here
	defaultModelFile = "models.json"
)

func main() {
	ctx := context.Background()

	modelFile := os.Getenv(MODEL_FILE_KEY)
	if modelFile == "" {
		modelFile = defaultModelFile
	}

	brain, err := llm.NewBrainLanguageModel(modelFile, MODEL_AUTH_KEY, MODEL_NAME_KEY, MODEL_LINK_KEY)
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
