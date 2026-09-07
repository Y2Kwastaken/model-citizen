package discordbot

import (
	"context"
	"log/slog"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"
)

const voiceConnectTimeout = 15 * time.Second

func handleJoin(_ discord.SlashCommandInteractionData, event *handler.CommandEvent) error {
	guild, ok := guildID(event)
	if !ok {
		return replyEphemeral(event, "This command only works in a server.")
	}

	_, ok = botVoiceChannel(event.Client(), guild)
	if ok {
		return replyEphemeral(event, "I'm already in a voice channel in this server.")
	}

	channel, ok := userVoiceChannel(event.Client(), guild, event.User().ID)
	if !ok {
		return replyEphemeral(event, "You need to be in a voice channel first.")
	}

	if err := event.DeferCreateMessage(true); err != nil {
		return err
	}

	// Connect off the event goroutine. disgo dispatches gateway events on a single
	// goroutine, so blocking here would stop the VoiceStateUpdate and
	// VoiceServerUpdate that Open is waiting on from ever being read -- the bot
	// appears in the channel but the handshake never completes.
	go connectVoice(context.WithoutCancel(event.Ctx), event, guild, channel)

	return nil
}

func handleLeave(_ discord.SlashCommandInteractionData, event *handler.CommandEvent) error {
	guild, ok := guildID(event)
	if !ok {
		return replyEphemeral(event, "This command only works in a server.")
	}

	botChannel, ok := botVoiceChannel(event.Client(), guild)
	if !ok {
		return replyEphemeral(event, "I'm not in a voice channel.")
	}

	userChannel, ok := userVoiceChannel(event.Client(), guild, event.User().ID)
	if !ok || userChannel != botChannel {
		return replyEphemeral(event, "You need to be in my voice channel to use this.")
	}

	conn := event.Client().VoiceManager.GetConn(guild)
	if conn == nil {
		if err := event.Client().UpdateVoiceState(event.Ctx, guild, nil, false, false); err != nil {
			return err
		}
		return replyEphemeral(event, "Left.")
	}

	conn.Close(event.Ctx)
	return replyEphemeral(event, "Left.")
}

func connectVoice(ctx context.Context, event *handler.CommandEvent, guild, channel snowflake.ID) {
	ctx, cancel := context.WithTimeout(ctx, voiceConnectTimeout)
	defer cancel()

	conn := event.Client().VoiceManager.CreateConn(guild)
	if err := conn.Open(ctx, channel, false, false); err != nil {
		slog.Error("opening voice connection",
			slog.String("guild_id", guild.String()),
			slog.String("channel_id", channel.String()),
			slog.Any("err", err),
		)
		// CreateConn registered the conn before Open ran; drop the dead entry so a
		// later attempt builds a fresh one instead of reusing this.
		event.Client().VoiceManager.RemoveConn(guild)
		editResponse(event, "Couldn't join that channel.")
		return
	}

	editResponse(event, "Joined.")
}
