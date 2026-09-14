package discordbot

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
)

const voiceConnectTimeout = 15 * time.Second

func handleVoiceLeaveEvent(event *events.GuildVoiceLeave) {
	guild := event.VoiceState.GuildID
	channelId, ok := botVoiceChannel(event.Client(), guild)
	if !ok {
		return
	}

	userIds := voiceChannelUsers(event.Client(), guild, channelId)
	if len(userIds) > 1 {
		return
	}

	if err := disconnectVoice(context.Background(), event.Client(), guild); err != nil {
		slog.Error("updating voice status", slog.Any("error", err))
	}
}

func handleJoin(_ discord.SlashCommandInteractionData, event *handler.CommandEvent) error {
	guild, ok := guildID(event)
	if !ok {
		return replyEphemeral(event, "This command only works in a server.")
	}

	if err := event.DeferCreateMessage(true); err != nil {
		return err
	}

	// Connect off the event goroutine. disgo dispatches gateway events on a single
	// goroutine, so blocking here would stop the VoiceStateUpdate and
	// VoiceServerUpdate that Open is waiting on from ever being read -- the bot
	// appears in the channel but the handshake never completes.
	go func() {
		if err := joinCaller(context.WithoutCancel(event.Ctx), event.Client(), guild, event.User().ID); err != nil {
			editResponse(event, err.Error())
			return
		}
		editResponse(event, "Joined.")
	}()

	return nil
}

func handleLeave(_ discord.SlashCommandInteractionData, event *handler.CommandEvent) error {
	guild, ok := guildID(event)
	if !ok {
		return replyEphemeral(event, "This command only works in a server.")
	}

	if err := leaveVoice(event.Ctx, event.Client(), guild, event.User().ID); err != nil {
		return replyEphemeral(event, err.Error())
	}

	return replyEphemeral(event, "Left.")
}

// joinCaller puts the bot in user's voice channel. It blocks on the handshake,
// so it must not run on the gateway goroutine (see handleJoin). The error,
// when there is one, is worded for whoever asked.
func joinCaller(ctx context.Context, client *bot.Client, guild snowflake.ID, user snowflake.ID) error {
	if _, ok := botVoiceChannel(client, guild); ok {
		return errors.New("I'm already in a voice channel in this server.")
	}

	channel, ok := userVoiceChannel(client, guild, user)
	if !ok {
		return errors.New("You need to be in a voice channel first.")
	}

	_, err := joinVoice(ctx, client, guild, channel)
	return err
}

// joinVoice connects the bot to channel and returns the connection. The real
// error is logged; the one returned is worded for whoever asked.
func joinVoice(ctx context.Context, client *bot.Client, guild snowflake.ID, channel snowflake.ID) (voice.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, voiceConnectTimeout)
	defer cancel()

	conn := client.VoiceManager.CreateConn(guild)
	if err := conn.Open(ctx, channel, false, false); err != nil {
		slog.Error("opening voice connection",
			slog.String("guild_id", guild.String()),
			slog.String("channel_id", channel.String()),
			slog.Any("err", err),
		)
		// CreateConn registered the conn before Open ran; drop the dead entry so a
		// later attempt builds a fresh one instead of reusing this.
		client.VoiceManager.RemoveConn(guild)
		return nil, errors.New("Couldn't join that channel.")
	}

	return conn, nil
}

// leaveVoice disconnects the bot from voice in guild, if user is in there with
// it. The error, when there is one, is worded for whoever asked.
func leaveVoice(ctx context.Context, client *bot.Client, guild snowflake.ID, user snowflake.ID) error {
	botChannel, ok := botVoiceChannel(client, guild)
	if !ok {
		return errors.New("I'm not in a voice channel.")
	}

	userChannel, ok := userVoiceChannel(client, guild, user)
	if !ok || userChannel != botChannel {
		return errors.New("You need to be in my voice channel to use this.")
	}

	return disconnectVoice(ctx, client, guild)
}

// disconnectVoice closes the guild's voice connection and drops its queue.
func disconnectVoice(ctx context.Context, client *bot.Client, guild snowflake.ID) error {
	conn := client.VoiceManager.GetConn(guild)
	if conn == nil {
		if err := client.UpdateVoiceState(ctx, guild, nil, false, false); err != nil {
			return err
		}
		removePlayer(guild)
		return nil
	}

	conn.Close(ctx)

	// Drop the queue after closing the connection, so the audio sender has
	// stopped before its reader is closed.
	removePlayer(guild)
	return nil
}

// voiceTarget decides what a playback request needs: the caller's channel, and
// whether the bot still has to join it. It only reads the cache, so it is safe
// on the gateway goroutine; the join itself is not.
func voiceTarget(client *bot.Client, guild snowflake.ID, user snowflake.ID) (channel snowflake.ID, join bool, err error) {
	userChannel, ok := userVoiceChannel(client, guild, user)
	if !ok {
		return 0, false, errors.New("you must be connected to a voice channel to do this.")
	}

	botChannel, inVoice := botVoiceChannel(client, guild)
	if inVoice && botChannel != userChannel {
		return 0, false, errors.New("you must be in the same channel as the bot to do this.")
	}

	return userChannel, !inVoice, nil
}

// ensureVoice returns the bot's voice connection in guild, joining user's
// channel first when it has none. It may block on the handshake.
func ensureVoice(ctx context.Context, client *bot.Client, guild snowflake.ID, user snowflake.ID) (voice.Conn, error) {
	channel, join, err := voiceTarget(client, guild, user)
	if err != nil {
		return nil, err
	}

	if join {
		return joinVoice(ctx, client, guild, channel)
	}

	conn := client.VoiceManager.GetConn(guild)
	if conn == nil || conn.ChannelID() == nil {
		return nil, errors.New("I've lost my voice connection -- try /leave then /join.")
	}
	return conn, nil
}
