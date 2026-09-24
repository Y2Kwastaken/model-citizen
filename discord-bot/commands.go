package discordbot

import (
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"

	"github.com/Y2Kwastaken/model-citizen/llm/model"
)

type Command struct {
	Create  discord.ApplicationCommandCreate
	Handler handler.SlashCommandHandler
}

// commandTable is built per bot: some handlers close over the brain.
func commandTable(brain model.LanguageModel) []Command {
	return []Command{
		{
			Create: discord.SlashCommandCreate{
				Name:        "ping",
				Description: "check if the bot is alive",
			},
			Handler: handlePing,
		},
		{
			Create: discord.SlashCommandCreate{
				Name:        "dump-memories",
				Description: "dumps bot memories",
			},
			Handler: func(data discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
				return dumpMemories(data, e, brain)
			},
		},
		{
			Create: discord.SlashCommandCreate{
				Name:        "memorize",
				Description: "writes a memory into the bot's head by hand",
				Options: []discord.ApplicationCommandOption{
					discord.ApplicationCommandOptionString{
						Name:        "name",
						Description: "who the memory is about",
						Required:    true,
					},
					discord.ApplicationCommandOptionString{
						Name:        "memory",
						Description: "what to remember about them",
						Required:    true,
					},
					discord.ApplicationCommandOptionInt{
						Name:        "type",
						Description: "which memory it goes in (default short term)",
						Required:    false,
						Choices: []discord.ApplicationCommandOptionChoiceInt{
							{Name: "short term", Value: int(model.SHORT_TERM)},
							{Name: "long term", Value: int(model.LONG_TERM)},
						},
					},
				},
			},
			Handler: memorizeCommand(brain),
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
				Name:        "say",
				Description: "says something out loud in the voice channel",
				Options: []discord.ApplicationCommandOption{
					discord.ApplicationCommandOptionString{
						Name:        "text",
						Description: "what to say",
						Required:    true,
					},
				},
			},
			Handler: sayCommand(brain),
		},
		{
			Create: discord.SlashCommandCreate{
				Name:        "leave",
				Description: "leaves the user's current voice call",
			},
			Handler: handleLeave,
		},
	}
}

func newRouter(brain model.LanguageModel) (*handler.Mux, []discord.ApplicationCommandCreate) {
	commands := commandTable(brain)
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

func memorizeCommand(brain model.LanguageModel) handler.SlashCommandHandler {
	return func(data discord.SlashCommandInteractionData, event *handler.CommandEvent) error {
		memType := model.MemoryType(data.Int("type"))
		memory := model.ModelMemory{
			At:     time.Now(),
			Name:   data.String("name"),
			Memory: data.String("memory"),
		}

		if !brain.MemorySet().Insert(memory, memType) {
			return replyEphemeral(event, "nothing backs "+memType.String()+" memory yet, so that went nowhere")
		}
		return replyEphemeral(event, memType.String()+": "+memory.Name+" -- "+memory.Memory)
	}
}

func dumpMemories(_ discord.SlashCommandInteractionData, event *handler.CommandEvent, brain model.LanguageModel) error {
	var sb strings.Builder

	sb.WriteString("Short Term:\n")
	for _, memory := range brain.MemorySet().AllOrderedMemories() {
		sb.WriteString(memory.At.Format("2006-01-02 15:04:05"))
		sb.WriteString(" ")
		sb.WriteString(memory.Name)
		sb.WriteString(" ")
		sb.WriteString(memory.Memory)
		sb.WriteString("\n")
	}

	return replyEphemeral(event, sb.String())
}
