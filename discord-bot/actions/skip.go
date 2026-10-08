package actions

import (
	"context"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/state"
)

func skipAction(services *state.GlobalServices) Action {
	return Action{
		Name:        "skip",
		Description: "skips the song playing now and moves on to the next one in the queue",
		Run:         skipHandler(services),
	}
}

func skipHandler(services *state.GlobalServices) func(context.Context, Request) (string, error) {
	return func(_ context.Context, req Request) (string, error) {
		if msg, ok := requireSameChannel(req); !ok {
			return msg, nil
		}

		guildServices, ok := services.GetGuild(req.Guild)
		if !ok {
			return "could not fetch guild services", nil
		}

		err := guildServices.Player.Skip()
		if err != nil {
			return err.Error(), nil
		} else {
			return "skipped the current song", nil
		}
	}
}
