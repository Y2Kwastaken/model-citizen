package discordbot

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/audio"
	"github.com/Y2Kwastaken/model-citizen/llm/model"
)

// The mixer of whatever is on a guild's connection, when something is: a
// song, or a line the bot is saying by itself. A line goes through it so the
// bot talks over the music instead of interrupting it, and a song that
// starts mid-line goes under the line (adoptStream) instead of cutting it.
var (
	mixersMu sync.Mutex
	mixers   = map[snowflake.ID]playing{}
)

type playing struct {
	mixer  *audio.Mixer
	stream *audio.OpusStream
	// a line by itself, which a song may take over
	solo bool
}

func setMixer(guild snowflake.ID, mixer *audio.Mixer, stream *audio.OpusStream, solo bool) {
	mixersMu.Lock()
	defer mixersMu.Unlock()
	mixers[guild] = playing{mixer, stream, solo}
}

func clearMixer(guild snowflake.ID) {
	mixersMu.Lock()
	defer mixersMu.Unlock()
	delete(mixers, guild)
}

// What became of a line's own mixer by the time the line was over.
type lineFate int

const (
	lineOwned    lineFate = iota // still the line's: the connection is free again
	lineAdopted                  // a song took it over, and the connection is the song's
	lineReplaced                 // a song took the connection instead; the line's stream is orphaned
)

// releaseMixer forgets a line's own mixer, if it is still the line's to forget.
func releaseMixer(guild snowflake.ID, mixer *audio.Mixer) lineFate {
	mixersMu.Lock()
	defer mixersMu.Unlock()
	current, ok := mixers[guild]
	switch {
	case !ok || current.mixer != mixer:
		return lineReplaced
	case !current.solo:
		return lineAdopted
	}
	delete(mixers, guild)
	return lineOwned
}

func mixerFor(guild snowflake.ID) (*audio.Mixer, bool) {
	mixersMu.Lock()
	defer mixersMu.Unlock()
	current, ok := mixers[guild]
	return current.mixer, ok
}

// adoptStream puts bed under a line the bot is saying by itself, and hands
// back the line's stream as the song's: it is already on the connection, so
// the song joins it rather than replacing it, which is what would cut the
// line off. It reports false when there is no such line, and the song needs
// a stream of its own.
func adoptStream(guild snowflake.ID, bed io.Reader) (*audio.OpusStream, bool) {
	mixersMu.Lock()
	defer mixersMu.Unlock()
	current, ok := mixers[guild]
	if !ok || !current.solo {
		return nil, false
	}
	// Under the lock, so a line that ends this instant either lands here
	// and is played over, or has already been released.
	origin, ok := current.mixer.SetBed(bed)
	if !ok {
		return nil, false
	}
	current.stream.SetOrigin(origin)
	current.solo = false
	mixers[guild] = current
	return current.stream, true
}

// speak says text in guild's voice channel and returns once it has been said.
//
// The bot must already be connected: this plays into the connection /join
// made rather than making one, so a line never drags the bot into a channel.
func speak(ctx context.Context, client *bot.Client, brain model.LanguageModel, guild snowflake.ID, text string) error {
	pcm, err := synthesize(ctx, brain, text)
	if err != nil {
		return err
	}
	return play(ctx, client, guild, pcm)
}

// synthesize is the voice's half of speak: text to PCM, nothing heard yet.
// The clip is whole when this returns; the PCM streams out of ffmpeg.
func synthesize(ctx context.Context, brain model.LanguageModel, text string) (io.ReadCloser, error) {
	if !brain.HasFeature(model.TTS) {
		return nil, fmt.Errorf("no voice is configured")
	}

	clip, err := brain.Speak(ctx, text)
	if err != nil {
		return nil, err
	}

	// Whatever the voice answered in, ffmpeg makes it what Discord takes.
	return audio.DecodeReader(bytes.NewReader(clip.Data))
}

// play is the channel's half of speak: it returns once pcm has been heard,
// which takes as long as the line is.
func play(ctx context.Context, client *bot.Client, guild snowflake.ID, pcm io.ReadCloser) error {
	conn := client.VoiceManager.GetConn(guild)
	if conn == nil {
		pcm.Close() // the decoder behind it is a process
		return fmt.Errorf("I'm not in a voice channel")
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

	stream, mixer, done, err := audio.StreamSpeech(pcm)
	if err != nil {
		return err
	}
	setMixer(guild, mixer, stream, true)

	conn.SetOpusFrameProvider(stream)
	select {
	case <-done:
	case <-ctx.Done():
	}

	// The line has been mixed; it has been heard once the stream has sent
	// it, at most a buffer later. Unless a song took the stream over, in
	// which case Done is the song's, or replaced it, in which case nothing
	// drains it and Done never comes.
	select {
	case <-stream.Done():
	case <-time.After(audio.BufferedAudio + time.Second):
	case <-ctx.Done():
	}

	switch releaseMixer(guild, mixer) {
	case lineOwned:
		stream.Close()
		conn.SetOpusFrameProvider(nil)
	case lineReplaced:
		stream.Close()
	case lineAdopted:
		// the song's now, and ends with it
	}
	return ctx.Err()
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
