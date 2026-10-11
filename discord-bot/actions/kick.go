package actions

import (
	"context"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
)

func kickAction() Action {
	return Action{
		Name:        "kick",
		Description: "disconnects a user from their voice channel",
		Params:      []Param{targetParam("who to disconnect")},
		Serious:     false,
		Run:         kickHandler,
	}
}

func kickHandler(_ context.Context, req Request) (string, error) {
	target, msg, ok := voiceTarget(req, discord.PermissionMoveMembers)
	if !ok {
		return msg, nil
	}

	if err := disconnect(req.Client, req.Guild, target.User.ID); err != nil {
		return err.Error(), nil
	}
	return "disconnected " + target.EffectiveName(), nil
}

// disgo's MemberUpdate leaves out a nil channel_id, so the null that
// disconnects someone is sent by hand
func disconnect(client *bot.Client, guild, user snowflake.ID) error {
	body := map[string]any{"channel_id": nil}
	return client.Rest.Do(rest.UpdateMember.Compile(nil, guild, user), body, nil)
}
