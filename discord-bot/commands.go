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
					Name:        "song",
					Description: "link to the song, or search terms",
					Required:    true,
				},
			},
		},
		Handler: handlePlay,
	},
	{
		Create: discord.SlashCommandCreate{
			Name:        "skip",
			Description: "skips the current song",
		},
		Handler: handleSkip,
	},
	{
		Create: discord.SlashCommandCreate{
			Name:        "rewind",
			Description: "rewinds to the start of the current song",
		},
		Handler: handleRewind,
	},
	{
		Create: discord.SlashCommandCreate{
			Name:        "transcribe",
			Description: "records you for a few seconds and reads back what the model heard",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionInt{
					Name:        "seconds",
					Description: "how long to record for (default 8, max 30)",
					Required:    false,
				},
			},
		},
		Handler: handleTranscribe,
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
