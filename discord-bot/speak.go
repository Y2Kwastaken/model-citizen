package discordbot

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/audio"
	"github.com/Y2Kwastaken/model-citizen/llm/model"
)

// The mixer of whatever is playing in a guild, when something is. A line goes
// through it so the bot talks over the music instead of interrupting it; with
// nothing playing there is no mixer and the line gets a stream of its own.
var (
	mixersMu sync.Mutex
	mixers   = map[snowflake.ID]*audio.Mixer{}
)

func setMixer(guild snowflake.ID, mixer *audio.Mixer) {
	mixersMu.Lock()
	defer mixersMu.Unlock()
	mixers[guild] = mixer
}

func clearMixer(guild snowflake.ID) {
	mixersMu.Lock()
	defer mixersMu.Unlock()
	delete(mixers, guild)
}

func mixerFor(guild snowflake.ID) (*audio.Mixer, bool) {
	mixersMu.Lock()
	defer mixersMu.Unlock()
	mixer, ok := mixers[guild]
	return mixer, ok
}

// speak says text in guild's voice channel and returns once it has been said.
//
// The bot must already be connected: this plays into the connection /join
// made rather than making one, so a line never drags the bot into a channel.
func speak(ctx context.Context, client *bot.Client, brain model.LanguageModel, guild snowflake.ID, text string) error {
	if !brain.HasFeature(model.TTS) {
		return fmt.Errorf("no voice is configured")
	}

	conn := client.VoiceManager.GetConn(guild)
	if conn == nil {
		return fmt.Errorf("I'm not in a voice channel")
	}

	clip, err := brain.Speak(ctx, text)
	if err != nil {
		return err
	}

	// Whatever the voice answered in, ffmpeg makes it what Discord takes.
	pcm, err := audio.DecodeReader(bytes.NewReader(clip.Data))
	if err != nil {
		return err
	}

	// Something playing: mix in and let the music carry on underneath.
	if mixer, ok := mixerFor(guild); ok {
		if done, accepted := mixer.Say(pcm); accepted {
			select {
			case <-done:
			case <-ctx.Done():
			}
			return nil
		}
		// The track ended between the lookup and the hand-off; fall through
		// and play it alone.
	}

	stream, err := audio.StreamSpeech(pcm)
	if err != nil {
		return err
	}

	conn.SetOpusFrameProvider(stream)
	select {
	case <-stream.Done():
	case <-ctx.Done():
		stream.Close()
	}

	// Only silence the connection if it is still ours: a song started during
	// a long line would own the provider by now.
	if _, playing := mixerFor(guild); !playing {
		conn.SetOpusFrameProvider(nil)
	}
	return nil
}

// sayCommand is how a voice gets tried without going through the whole listen
// pipeline: type a line, hear it.
func sayCommand(brain model.LanguageModel) handler.SlashCommandHandler {
	return func(data discord.SlashCommandInteractionData, event *handler.CommandEvent) error {
		guild, ok := guildID(event)
		if !ok {
			return replyEphemeral(event, "This command only works in a server.")
		}

		if !brain.HasFeature(model.TTS) {
			return replyEphemeral(event, "I have no voice configured -- speech-models.json isn't usable.")
		}

		if err := requireSharedVoice(event.Client(), guild, event.User().ID); err != nil {
			return replyEphemeral(event, err.Error())
		}

		text := data.String("text")
		if err := event.DeferCreateMessage(true); err != nil {
			return err
		}

		// Synthesis and playback both outlast the interaction, so this leaves
		// the gateway goroutine (see handleJoin).
		go func() {
			ctx := context.WithoutCancel(event.Ctx)
			if err := speak(ctx, event.Client(), brain, guild, text); err != nil {
				slog.Error("saying a line", slog.String("guild_id", guild.String()), slog.Any("err", err))
				editResponse(event, "I couldn't say that: "+err.Error())
				return
			}
			editResponse(event, "Said it.")
		}()

		return nil
	}
}
