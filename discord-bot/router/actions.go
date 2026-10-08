package router

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/actions"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
)

// CommandsFrom turns every action into a slash command.
func CommandsFrom(all []actions.Action) []Command {
	commands := make([]Command, len(all))
	for i, action := range all {
		commands[i] = commandFrom(action)
	}
	return commands
}

func commandFrom(action actions.Action) Command {
	options := make([]discord.ApplicationCommandOption, len(action.Params))
	for i, param := range action.Params {
		options[i] = discord.ApplicationCommandOptionString{
			Name:        param.Name,
			Description: param.Description,
			Required:    param.Required,
		}
	}

	return Command{
		Create: discord.SlashCommandCreate{
			Name:        action.Name,
			Description: action.Description,
			Options:     options,
		},
		Handler: actionHandler(action),
	}
}

// defers the reply then runs the action off the interaction, actions can be
// slower than the 3 seconds discord gives to answer
func actionHandler(action actions.Action) func(discord.SlashCommandInteractionData, *handler.CommandEvent) error {
	return func(data discord.SlashCommandInteractionData, event *handler.CommandEvent) error {
		guild := event.GuildID()
		if guild == nil {
			return fmt.Errorf("%s only works in a server", action.Name)
		}

		args := make(actions.Args, len(action.Params))
		for _, param := range action.Params {
			if value, ok := data.OptString(param.Name); ok {
				args[param.Name] = value
			}
		}

		req := actions.Request{
			Client: event.Client(),
			Guild:  *guild,
			User:   event.Member().User.ID,
			Args:   args,
		}

		if err := event.DeferCreateMessage(true); err != nil {
			return err
		}

		go func() {
			msg, err := action.Run(context.Background(), req)
			if err != nil {
				slog.Error("running action", slog.String("action", action.Name), slog.Any("error", err))
				msg = err.Error()
			}
			updateReply(event, msg)
		}()

		return nil
	}
}

func updateReply(event *handler.CommandEvent, content string) {
	_, err := event.UpdateInteractionResponse(
		discord.NewMessageUpdateV2().
			AddComponents(
				discord.NewTextDisplay(content),
			),
	)

	if err != nil {
		slog.Error("updating interaction response", slog.String("command", event.Data.CommandName()), slog.Any("error", err))
	}
}
