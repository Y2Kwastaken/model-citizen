package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/actions"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/audio"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/config"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/events"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/network"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/router"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/state"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/tools"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/wake"
	"github.com/Y2Kwastaken/model-citizen/shared"
	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/cache"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/godave/golibdave"
)

const (
	DISCORD_KEY          = "DISCORD_KEY"
	MODEL_CITIZEN_CONFIG = "MODEL_CITIZEN_CONFIG"
	LLM_ADDRESS          = "LLM_ADDRESS"
)

func main() {
	cfg, err := config.Load(shared.EnvOrVal(MODEL_CITIZEN_CONFIG, "config/model-citizen.toml"))
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	slog.SetLogLoggerLevel(cfg.Logging.SlogLevel())

	globals, err := state.InitGlobalServices(cfg)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	services := &globals

	// doesn't connect until first use so a down llm won't stop the bot
	llm, err := network.New(shared.EnvOrVal(LLM_ADDRESS, "localhost:50051"))
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer llm.Close()
	services.Brain = llm

	// a missing wake word only stops the bot listening
	listen := cfg.Listen
	model, err := wake.Load(listen.Library, listen.Directory, listen.Model)
	if err != nil {
		slog.Warn("wake word disabled, not listening in voice", slog.Any("error", err))
	} else {
		services.Wake = model
	}

	slog.Info("Setting up music service; this may take a while...")
	// a failed download only turns music off, the rest of the bot still runs
	if err := services.Music.Setup(context.Background()); err != nil {
		slog.Error("music disabled, setting up the music service failed", slog.Any("error", err))
	} else {
		slog.Info("Finalized setting up music service")
	}

	token, err := shared.EnvOrErr(DISCORD_KEY)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	all := actions.All(services)
	cmdRouter, cmds := router.Router(router.CommandsFrom(all))
	client, err := disgo.New(token,
		bot.WithGatewayConfigOpts(
			gateway.WithIntents(
				gateway.IntentGuilds,
				gateway.IntentGuildVoiceStates,
				gateway.IntentGuildMessages,
				gateway.IntentMessageContent,
			)),
		bot.WithCacheConfigOpts(
			cache.WithCaches(
				cache.FlagVoiceStates,
			),
		),
		bot.WithVoiceManagerConfigOpts(
			voice.WithDaveSessionCreateFunc(golibdave.NewSession),
			// the default receiver logs every undecryptable packet as an error
			voice.WithConnConfigOpts(voice.WithConnAudioReceiverCreateFunc(audio.NewAudioReceiver)),
		),
		bot.WithEventListeners(cmdRouter,
			router.SyncOnReady(cmds, services),
			router.SyncOnJoin(cmds, services),
			events.ChatListener(services),
		),
	)

	if err != nil {
		slog.Error("failed to start", slog.Any("error", err))
		os.Exit(1)
	}
	// set before the gateway opens, guilds register as soon as it does
	services.OnWake = events.WakeHandler(client, services)

	background := context.Background()
	if err = client.OpenGateway(background); err != nil {
		slog.Error("failed to start", slog.Any("error", err))
		os.Exit(1)
	}
	defer client.Close(background)

	toolCtx, stopTools := context.WithCancel(background)
	defer stopTools()
	go llm.RegisterTools(toolCtx, tools.ToolsFrom(all, client))

	slog.Info("model-citizen is now running. Press CTRL-C to exit.")
	// make a channel to detect an os signal
	s := make(chan os.Signal, 1)
	// notify our channel when sigint, sigterm or interrupt occurs
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	// we then return our signal to this point
	<-s
	// we could run more logic here
}
