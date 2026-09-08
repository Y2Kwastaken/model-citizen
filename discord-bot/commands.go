package discordbot

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
)

type Command struct {
	Create  discord.ApplicationCommandCreate
	Handler handler.SlashCommandHandler
}

var commands = []Command{
	{
		Create: discord.SlashCommandCreate{
			Name:        "ping",
			Description: "check if the bot is alive",
		},
		Handler: handlePing,
	},
	{
		Create: discord.SlashCommandCreate{
			Name:        "join",
			Description: "joins the user's current voice call",
		},
		Handler: handleJoin,
	},
	{
		Create: discord.SlashCommandCreate{
			Name:        "play",
			Description: "queues a song and starts playback",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionString{
					Name:        "url",
					Description: "link to the song",
					Required:    true,
				},
			},
		},
		Handler: handlePlay,
	},
	{
		Create: discord.SlashCommandCreate{
			Name:        "leave",
			Description: "leaves the user's current voice call",
		},
		Handler: handleLeave,
	},
}

func newRouter() (*handler.Mux, []discord.ApplicationCommandCreate) {
	router := handler.New()
	creates := make([]discord.ApplicationCommandCreate, len(commands))
	for i, cmd := range commands {
		router.SlashCommand("/"+cmd.Create.CommandName(), cmd.Handler)
		creates[i] = cmd.Create
	}
	return router, creates
}

func handlePing(_ discord.SlashCommandInteractionData, event *handler.CommandEvent) error {
	return replyEphemeral(event, "pong")
}
