package router

import (
	"log/slog"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/state"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"
)

type Command struct {
	Create  discord.ApplicationCommandCreate
	Handler handler.SlashCommandHandler
}

func Router(commands []Command) (*handler.Mux, []discord.ApplicationCommandCreate) {
	router := handler.New()
	createdCommands := make([]discord.ApplicationCommandCreate, len(commands))
	for i, cmd := range commands {
		router.SlashCommand("/"+cmd.Create.CommandName(), cmd.Handler)
		createdCommands[i] = cmd.Create
	}

	return router, createdCommands
}

func SyncOnReady(cmds []discord.ApplicationCommandCreate, services *state.GlobalServices) bot.EventListener {
	return bot.NewListenerFunc(func(e *events.GuildReady) {
		syncCommands(e.Client(), e.GuildID, cmds)
		services.RegisterGuild(e.GuildID)
	})
}

func SyncOnJoin(cmds []discord.ApplicationCommandCreate, services *state.GlobalServices) bot.EventListener {
	return bot.NewListenerFunc(func(e *events.GuildJoin) {
		syncCommands(e.Client(), e.GuildID, cmds)
		services.RegisterGuild(e.GuildID)
	})
}

func syncCommands(client *bot.Client, guildID snowflake.ID, creates []discord.ApplicationCommandCreate) {
	if _, err := client.Rest.SetGuildCommands(client.ApplicationID, guildID, creates); err != nil {
		slog.Error("syncing guild commands",
			slog.String("guild_id", guildID.String()),
			slog.Any("err", err),
		)
		return
	}
	slog.Info("synced guild commands", slog.String("guild_id", guildID.String()))
}
