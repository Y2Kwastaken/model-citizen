package actions

import (
	"log/slog"
	"strings"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/voice"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
)

// the param naming who a voice moderation action is used on
func targetParam(description string) Param {
	return Param{
		Name:        "user",
		Description: description + ": a mention, id, username or nickname",
		Required:    true,
	}
}

// finds the member in voice the request's user arg names and, for slash
// commands, checks the person asking has permission over them in their
// channel. the message is the refusal
func voiceTarget(req Request, permission discord.Permissions) (*discord.Member, string, bool) {
	client := req.Client
	query := strings.TrimSpace(req.Args.String("user"))

	target, msg, ok := findVoiceMember(client, req.Guild, query)
	if !ok {
		return nil, msg, false
	}
	if target.User.ID == client.ID() {
		return nil, "the bot won't do that to itself", false
	}

	// only the llm runs actions in the background, and it acts on its own
	// judgement, so only discord's check on the bot applies
	if req.Background {
		return target, "", true
	}

	channel, ok := voice.UserChannel(client, target.User.ID, &req.Guild)
	if !ok {
		return nil, target.EffectiveName() + " isn't in a voice channel", false
	}

	if msg, ok := requirePermission(client, req.Guild, req.User, *channel, permission); !ok {
		return nil, msg, false
	}
	return target, "", true
}

// finds the one member in voice that query names, by mention, id, username or nickname
func findVoiceMember(client *bot.Client, guild snowflake.ID, query string) (*discord.Member, string, bool) {
	// collected first so the cache isn't held during the rest calls
	var users []snowflake.ID
	for state := range client.Caches.VoiceStates(guild) {
		if state.ChannelID != nil {
			users = append(users, state.UserID)
		}
	}

	id, isID := mentionID(query)
	var found []*discord.Member
	for _, user := range users {
		member, err := client.Rest.GetMember(guild, user)
		if err != nil {
			slog.Warn("fetching voice member", slog.String("user_id", user.String()), slog.Any("error", err))
			continue
		}
		if (isID && member.User.ID == id) || namedBy(*member, query) {
			found = append(found, member)
		}
	}

	switch len(found) {
	case 0:
		return nil, "nobody called " + query + " is in a voice channel", false
	case 1:
		return found[0], "", true
	}
	return nil, "more than one user in voice goes by " + query, false
}

// the user id in a mention like <@123> or <@!123>, or a bare id
func mentionID(query string) (snowflake.ID, bool) {
	trimmed := strings.TrimPrefix(query, "<@")
	trimmed = strings.TrimPrefix(trimmed, "!")
	trimmed = strings.TrimSuffix(trimmed, ">")

	id, err := snowflake.Parse(trimmed)
	return id, err == nil
}

// whether name is the member's username, display name or nickname, ignoring case
func namedBy(member discord.Member, name string) bool {
	names := []string{member.User.Username}
	if member.User.GlobalName != nil {
		names = append(names, *member.User.GlobalName)
	}
	if member.Nick != nil {
		names = append(names, *member.Nick)
	}

	for _, candidate := range names {
		if strings.EqualFold(candidate, name) {
			return true
		}
	}
	return false
}

// the refusal when user lacks permission in channel, ok when they have it.
// discord only checks the bot, so the person asking is checked here
func requirePermission(client *bot.Client, guild, user, channel snowflake.ID, permission discord.Permissions) (string, bool) {
	if user == 0 {
		return "couldn't tell who asked, so nothing was done", false
	}

	member, err := client.Rest.GetMember(guild, user)
	if err != nil {
		return "couldn't check your permissions", false
	}

	voiceChannel, ok := client.Caches.Channel(channel)
	if !ok {
		return "couldn't check your permissions", false
	}

	missing := permission.Remove(client.Caches.MemberPermissionsInChannel(voiceChannel, *member))
	if missing != discord.PermissionsNone {
		return "you need " + missing.String() + " to do that", false
	}
	return "", true
}
