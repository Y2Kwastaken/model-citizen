package actions

import (
	"context"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/state"
)

func stopAction(services *state.GlobalServices) Action {
	return Action{
		Name:        "stop",
		Description: "stops the music, ending the current song and clearing the whole queue",
		Run:         stopHandler(services),
	}
}

func stopHandler(services *state.GlobalServices) func(context.Context, Request) (string, error) {
	return func(_ context.Context, req Request) (string, error) {
		if msg, ok := requireSameChannel(req); !ok {
			return msg, nil
		}

		guildServices, ok := services.GetGuild(req.Guild)
		if !ok {
			return "could not fetch guild services", nil
		}

		err := guildServices.Player.Stop()
		if err != nil {
			return err.Error(), nil
		} else {
			return "left", nil
		}
	}
}
