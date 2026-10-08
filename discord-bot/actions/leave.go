package actions

import (
	"context"
	"fmt"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/voice"
)

func leaveAction() Action {
	return Action{
		Name:        "leave",
		Description: "leaves the user's voice channel",
		Run:         leaveHandler,
	}
}

func leaveHandler(ctx context.Context, req Request) (string, error) {
	client := req.Client

	_, result := voice.SameChannel(client, client.ID(), req.User, &req.Guild)
	switch result {
	case voice.FirstNotPresent, voice.NeitherPresent:
		return "no voice channel to leave", nil
	case voice.SecondNotPresent:
		return "you must be in a voice channel to do this", nil
	case voice.DifferentPlace:
		return "you must be in the same channel as the bot to do this", nil
	case voice.BothPresent:
		err := voice.Leave(client, &req.Guild, ctx)
		if err != nil {
			return err.Error(), nil
		}

		return "left voice channel", nil
	}

	return "", fmt.Errorf("unknown path on leave")
}
