package actions

import (
	"context"
	"fmt"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/state"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/voice"
)

func joinAction(services *state.GlobalServices) Action {
	return Action{
		Name:        "join",
		Description: "joins the user's voice channel",
		Run:         joinHandler(services),
	}
}

func joinHandler(services *state.GlobalServices) func(context.Context, Request) (string, error) {
	return func(ctx context.Context, req Request) (string, error) {
		client := req.Client

		_, status := voice.SameChannel(client, req.User, client.ID(), &req.Guild)
		switch status {
		case voice.Uncertain:
			return "uncertain channel status of you and bot", nil
		case voice.FirstNotPresent:
			return "you must be in a voice channel to use this command", nil
		case voice.SecondNotPresent:
			channel, ok := voice.UserChannel(client, req.User, &req.Guild)
			if !ok {
				return "failed to get your voice channel", nil
			}

			guildService, ok := services.GetGuild(req.Guild)
			if !ok {
				return "failed to fetch services for this guild", nil
			}

			err := voice.Join(guildService.Mixer, guildService.Listener, client, channel, &req.Guild, ctx, services.Config.Voice.JoinTimeout)
			if err != nil {
				return err.Error(), nil
			}
			return "joined", nil
		case voice.BothPresent:
			return "you are already in a voice channel with this bot", nil
		case voice.DifferentPlace:
			return "this bot is already in another voice channel", nil
		case voice.NeitherPresent:
			return "you are not in a voice channel", nil
		}

		return "", fmt.Errorf("unknown path on join")
	}
}
