package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/gateway"
)

func main() {
	client, err := disgo.New("token",
		bot.WithGatewayConfigOpts(
			gateway.WithIntents(
				gateway.IntentsGuild,
				gateway.IntentGuildMessages,
				gateway.IntentDirectMessages,
			),
		),
	)

	if err != nil {
		panic(err)
	}

	if err = client.OpenGateway(context.TODO()); err != nil {
		panic(err)
	}

	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM)
	<-s
}
