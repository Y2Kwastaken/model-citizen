package actions

import (
	"context"

	"github.com/disgoorg/disgo/discord"
)

func disableAction() Action {
	return Action{
		Name:        "disable",
		Description: "server mutes and deafens a user in voice",
		Params:      []Param{targetParam("who to mute and deafen")},
		Serious:     false,
		Run:         disableHandler,
	}
}

func disableHandler(_ context.Context, req Request) (string, error) {
	target, msg, ok := voiceTarget(req, discord.PermissionMuteMembers|discord.PermissionDeafenMembers)
	if !ok {
		return msg, nil
	}

	on := true
	update := discord.MemberUpdate{Mute: &on, Deaf: &on}
	if _, err := req.Client.Rest.UpdateMember(req.Guild, target.User.ID, update); err != nil {
		return err.Error(), nil
	}
	return "muted and deafened " + target.EffectiveName(), nil
}
