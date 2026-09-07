package discordbot

import (
	"log/slog"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
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
