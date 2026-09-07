package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	discordbot "github.com/Y2Kwastaken/model-citizen/discord-bot"
)

func main() {
	ctx := context.Background()

	client, err := discordbot.Start(ctx, "DISCORD_KEY")

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
