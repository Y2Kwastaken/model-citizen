package discordbot

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/cache"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/godave/golibdave"
	"github.com/disgoorg/snowflake/v2"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/agent"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/audio"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/music"
	"github.com/Y2Kwastaken/model-citizen/llm/model"
)

// Listening is how the bot hears voice. Wake must not be nil.
type Listening struct {
	Wake      *audio.WakeWord
	Threshold float32 // score that counts as the wake word

	ListenWindow  time.Duration // retained voice length per speaker
	ContextWindow time.Duration // how much before a wake goes with it
	CommandQuiet  time.Duration // silence that ends a command
	CommandMax    time.Duration // longest command listened to
	MinUtterance  time.Duration // shorter is noise
	WakeDebounce  time.Duration
	Language      string // empty lets the transcriber guess

	TranscribeTimeout time.Duration
	SpeakTimeout      time.Duration // synthesis plus playback of one reply
}

// hearing is set once by Start.
var hearing Listening

// Music is how songs are queued, cached and played.
type Music struct {
	MaxQueueLength  int // zero or less is unlimited
	CacheRetention  int // zero or less keeps every download
	PrefetchAhead   int
	PlaybackTimeout time.Duration
	Volume          float32 // how loud songs play, 0 to 1
	DuckedGain      float32 // music volume under the bot's speech
}

// Start connects the bot.
func Start(ctx context.Context, brain model.LanguageModel, tokenVariable string, replyTimeout time.Duration, listening Listening, musicTunes Music) (*bot.Client, error) {
	slog.Info("disgo version", slog.String("version", disgo.Version))
	ears.brain, ears.wake, ears.threshold, ears.replyTimeout = brain, listening.Wake, listening.Threshold, replyTimeout
	hearing = listening
	tunes = musicTunes
	audio.DuckedGain = musicTunes.DuckedGain
	audio.MusicVolume = musicTunes.Volume

	token := os.Getenv(tokenVariable)
	if token == "" {
		return nil, fmt.Errorf("key not set in environment at variable %s", tokenVariable)
	}

	if err := music.EnsureYtdlp(ctx); err != nil {
		return nil, err
	}

	router, commands := newRouter(brain)

	client, err := disgo.New(token,
		bot.WithGatewayConfigOpts(
			gateway.WithIntents(
				gateway.IntentGuilds,
				gateway.IntentGuildMessages,
				gateway.IntentMessageContent,
				gateway.IntentGuildVoiceStates,
				gateway.IntentGuildMembers,
				gateway.IntentGuildModeration,
			),
		),
		bot.WithCacheConfigOpts(
			cache.WithCaches(cache.FlagGuilds, cache.FlagVoiceStates, cache.FlagMembers),
		),
		// discord requires libdave as vc is e2ee
		bot.WithVoiceManagerConfigOpts(
			voice.WithDaveSessionCreateFunc(golibdave.NewSession),
			// see dave.go: the silence tails DAVE cannot decrypt stop here
			voice.WithConnConfigOpts(voice.WithUDPConnCreateFunc(newQuietUDP)),
		),
		bot.WithEventListeners(router),
		bot.WithEventListenerFunc(func(e *events.GuildReady) {
			syncGuildCommands(e.Client(), e.GuildID, commands)
		}),
		bot.WithEventListenerFunc(func(e *events.GuildJoin) {
			syncGuildCommands(e.Client(), e.GuildID, commands)
		}),
		bot.WithEventListenerFunc(handleVoiceLeaveEvent),
		bot.WithEventListenerFunc(func(e *events.GuildMessageCreate) {
			agent.HandleMessage(brain, e, replyTimeout)
		}),
	)

	if err != nil {
		return nil, fmt.Errorf("building disgo client: %w", err)
	}

	registerTools(client, brain)

	if err = client.OpenGateway(ctx); err != nil {
		client.Close(ctx)
		return nil, fmt.Errorf("connecting to gateway: %w", err)
	}

	return client, nil
}

func syncGuildCommands(client *bot.Client, guildID snowflake.ID, creates []discord.ApplicationCommandCreate) {
	if _, err := client.Rest.SetGuildCommands(client.ApplicationID, guildID, creates); err != nil {
		slog.Error("syncing guild commands",
			slog.String("guild_id", guildID.String()),
			slog.Any("err", err),
		)
		return
	}
	slog.Info("synced guild commands", slog.String("guild_id", guildID.String()))
}
