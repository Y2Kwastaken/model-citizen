package voice

import (
	"context"
	"fmt"
	"time"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/audio"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/snowflake/v2"
)

const (
	leaveTimeout = 5 * time.Second
)

type Result int

const (
	Uncertain Result = iota
	FirstNotPresent
	SecondNotPresent
	NeitherPresent
	DifferentPlace
	BothPresent
)

func SameChannel(client *bot.Client, first snowflake.ID, second snowflake.ID, guild *snowflake.ID) (*snowflake.ID, Result) {
	result := Uncertain
	channelFirst, ok := UserChannel(client, first, guild)
	if !ok {
		result += FirstNotPresent
	}

	channelSecond, ok := UserChannel(client, second, guild)
	if !ok {
		result += SecondNotPresent
	}

	switch result {
	case NeitherPresent:
		return nil, NeitherPresent
	case FirstNotPresent:
		return nil, FirstNotPresent
	case SecondNotPresent:
		return nil, SecondNotPresent
	}

	// firstNotPresent secondNotPresent guarenteed channel nonNull for both
	// compare values not pointers
	if *channelFirst == *channelSecond {
		return channelFirst, BothPresent
	} else {
		return nil, DifferentPlace
	}
}

func UserChannel(client *bot.Client, user snowflake.ID, guild *snowflake.ID) (*snowflake.ID, bool) {
	state, ok := client.Caches.VoiceState(*guild, user)
	if !ok || state.ChannelID == nil {
		return nil, false
	}
	return state.ChannelID, true
}

func BotChannel(client *bot.Client, guild *snowflake.ID) (*snowflake.ID, bool) {
	return UserChannel(client, client.ID(), guild)
}

func Join(mixer *audio.OpusMixer, listener *audio.OpusListener, client *bot.Client, channel *snowflake.ID, guild *snowflake.ID, ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn := client.VoiceManager.CreateConn(*guild)
	err := conn.Open(ctx, *channel, false, false)
	if err != nil {
		client.VoiceManager.RemoveConn(*guild)
		return err
	}
	conn.SetOpusFrameProvider(mixer)
	if listener != nil {
		conn.SetOpusFrameReceiver(listener)
	}

	return nil
}

func Leave(client *bot.Client, guild *snowflake.ID, ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, leaveTimeout)
	defer cancel()

	conn := client.VoiceManager.GetConn(*guild)
	if conn != nil {
		conn.Close(ctx)
		return nil
	}

	return fmt.Errorf("could not close non existent connection")
}
