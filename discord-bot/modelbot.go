package discordbot

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/cache"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/godave/golibdave"
	"github.com/disgoorg/snowflake/v2"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/music"
)

func Start(ctx context.Context, tokenVariable string) (*bot.Client, error) {
	slog.Info("disgo version", slog.String("version", disgo.Version))

	token := os.Getenv(tokenVariable)
	if token == "" {
		return nil, fmt.Errorf("key not set in environment at variable %s", tokenVariable)
	}

	if err := music.EnsureYtdlp(ctx); err != nil {
		return nil, err
	}

	router, commands := newRouter()

	client, err := disgo.New(token,
		bot.WithGatewayConfigOpts(
			gateway.WithIntents(
				gateway.IntentGuilds,
				gateway.IntentGuildMessages,
				gateway.IntentMessageContent,
				gateway.IntentGuildVoiceStates,
			),
		),
		bot.WithCacheConfigOpts(
			cache.WithCaches(cache.FlagGuilds, cache.FlagVoiceStates),
		),
		// discord requires libdave as vc is e2ee
		bot.WithVoiceManagerConfigOpts(
			voice.WithDaveSessionCreateFunc(golibdave.NewSession),
		),
		bot.WithEventListeners(router),
		bot.WithEventListenerFunc(func(e *events.GuildReady) {
			syncGuildCommands(e.Client(), e.GuildID, commands)
		}),
		bot.WithEventListenerFunc(func(e *events.GuildJoin) {
			syncGuildCommands(e.Client(), e.GuildID, commands)
		}),
	)

	if err != nil {
		return nil, fmt.Errorf("building disgo client: %w", err)
	}

	if err = client.OpenGateway(ctx); err != nil {
		client.Close(ctx)
		return nil, fmt.Errorf("connecting to gateway: %w", err)
	}

	return client, nil
}

func syncGuildCommands(client *bot.Client, guildID snowflake.ID, creates []discord.ApplicationCommandCreate) {
	if _, err := client.Rest.SetGuildCommands(client.ApplicationID, guildID, creates); err != nil {
		slog.Error("syncing guild commands",
			slog.String("guild_id", guildID.String()),
			slog.Any("err", err),
		)
		return
	}
	slog.Info("synced guild commands", slog.String("guild_id", guildID.String()))
}
