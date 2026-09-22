package discordbot

import (
	"errors"
	"log/slog"
	"net/url"
	"strings"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
)

// reply responds to the interaction with a message visible to the whole channel.
//
// This is an initial response, so it must be sent within 3 seconds of the
// interaction. Use editResponse instead once the response has been deferred.
func reply(event *handler.CommandEvent, content string) error {
	return event.CreateMessage(discord.NewMessageCreate().WithContent(content))
}

// replyEphemeral responds to the interaction with a message only the invoking
// user can see. Ephemeral responses cannot be sent through Rest.CreateMessage,
// and a response cannot be switched between ephemeral and public after the fact.
func replyEphemeral(event *handler.CommandEvent, content string) error {
	return event.CreateMessage(discord.NewMessageCreate().
		WithContent(content).
		WithEphemeral(true))
}

// editResponse replaces the content of an interaction response that has already
// been sent or deferred. It logs rather than returns failures, so it is safe to
// call from a goroutine that has no caller left to return an error to.
//
// The response inherits the visibility chosen when it was created: editing a
// deferred public response cannot make it ephemeral.
func editResponse(event *handler.CommandEvent, content string) {
	_, err := event.UpdateInteractionResponse(discord.NewMessageUpdate().WithContent(content))
	if err != nil {
		slog.Error("updating interaction response",
			slog.String("command", event.Data.CommandName()),
			slog.Any("err", err),
		)
	}
}

// guildID returns the guild a command was invoked in. The second return is false
// in DMs, where commands that touch guild state cannot run.
func guildID(event *handler.CommandEvent) (snowflake.ID, bool) {
	id := event.GuildID()
	if id == nil {
		return 0, false
	}
	return *id, true
}

// userVoiceChannel returns the voice channel the given user is currently in.
//
// This reads the cache, so it requires gateway.IntentGuildVoiceStates and
// cache.FlagVoiceStates. The second return is false when the user is not
// connected to voice in this guild.
func userVoiceChannel(client *bot.Client, guild snowflake.ID, user snowflake.ID) (snowflake.ID, bool) {
	state, ok := client.Caches.VoiceState(guild, user)
	if !ok || state.ChannelID == nil {
		return 0, false
	}
	return *state.ChannelID, true
}

// botVoiceChannel returns the voice channel the bot is currently connected to in
// the given guild.
//
// This reads the voice state cache rather than the voice manager, so it reflects
// Discord's view of where the bot is. It goes false as soon as a moderator
// disconnects the bot, whereas VoiceManager.GetConn would still hand back a
// connection whose ChannelID is nil.
func botVoiceChannel(client *bot.Client, guild snowflake.ID) (snowflake.ID, bool) {
	return userVoiceChannel(client, guild, client.ID())
}

// validURL reports whether raw is a fetchable http(s) URL.
//
// This is a cheap syntactic check, not a liveness one: it rejects junk like
// "HI!!!!" without a network round trip, but a well-formed link to a site with
// no media still fails later at download time.
func validURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

// makes a check to ensure the bot and user are in the same voice channel in the given guild
//
// reads the voice channel state of voice channel in the discord guild.
// returns the bot channel id, user channel id, and true if in same vc, otherwise false.
func ensureBotInUserVoice(client *bot.Client, guild snowflake.ID, user snowflake.ID) (*snowflake.ID, *snowflake.ID, bool) {
	botChannel, ok := botVoiceChannel(client, guild)
	var botChannelPtr *snowflake.ID
	if ok {
		botChannelPtr = &botChannel
	} else {
		botChannelPtr = nil
	}

	userChannel, ok := userVoiceChannel(client, guild, user)
	var userChannelPtr *snowflake.ID
	if ok {
		userChannelPtr = &userChannel
	} else {
		userChannelPtr = nil
	}

	return botChannelPtr, userChannelPtr, botChannelPtr != nil && userChannelPtr != nil && *botChannelPtr == *userChannelPtr
}

// requireSharedVoice is the precondition for touching playback: the bot is in
// a voice channel, and user is in the same one. The error is worded for
// whoever asked, so it can be shown to them as is.
func requireSharedVoice(client *bot.Client, guild snowflake.ID, user snowflake.ID) error {
	botChannel, userChannel, inSame := ensureBotInUserVoice(client, guild, user)
	switch {
	case botChannel == nil:
		return errors.New("bot must be connected to a voice channel to do this.")
	case userChannel == nil:
		return errors.New("you must be connected to a voice channel to do this.")
	case !inSame:
		return errors.New("you must be in the same channel as the bot to do this.")
	}
	return nil
}

// voiceChannelUsers returns the users currently connected to the given voice
// channel.
//
// disgo's voice state cache is keyed by guild, not channel, so the channel
// filter happens here. Like userVoiceChannel this reads the cache, so it needs
// gateway.IntentGuildVoiceStates and cache.FlagVoiceStates.
func voiceChannelUsers(client *bot.Client, guild snowflake.ID, channel snowflake.ID) []snowflake.ID {
	var users []snowflake.ID
	for state := range client.Caches.VoiceStates(guild) {
		if state.ChannelID != nil && *state.ChannelID == channel {
			users = append(users, state.UserID)
		}
	}
	return users
}

// findApproximateUsersInVoice returns the users in a voice channel whose
// username, nickname or display name matches name, ignoring case. Cache
// first; a miss is a REST call, and a channel full of misses rate limits.
func findApproximateUsersInVoice(client *bot.Client, guild snowflake.ID, channel snowflake.ID, name string) []discord.User {
	var users []discord.User
	for _, user := range voiceChannelUsers(client, guild, channel) {
		member, ok := client.Caches.Member(guild, user)
		if !ok {
			fetched, err := client.Rest.GetMember(guild, user)
			if err != nil {
				slog.Warn("looking up voice member", slog.String("user_id", user.String()), slog.Any("err", err))
				continue
			}
			member = *fetched
		}

		nick := ""
		if member.Nick != nil {
			nick = *member.Nick
		}
		if strings.EqualFold(nick, name) ||
			strings.EqualFold(member.User.Username, name) ||
			strings.EqualFold(member.EffectiveName(), name) {
			users = append(users, member.User)
		}
	}

	return users
}

// disconnectFromVoice kicks user out of whatever voice channel they are in.
// MemberUpdate omits a nil ChannelID, and Discord needs an explicit null.
func disconnectFromVoice(client *bot.Client, guild snowflake.ID, user snowflake.ID) error {
	body := struct {
		ChannelID *snowflake.ID `json:"channel_id"`
	}{}
	return client.Rest.Do(rest.UpdateMember.Compile(nil, guild, user), body, nil)
}
