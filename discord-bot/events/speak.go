package events

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/state"
	saudio "github.com/Y2Kwastaken/model-citizen/shared/audio"
)

const (
	// music plays on layer 0
	speechLayer uint8 = 1
	// above the llm's own tts.timeout, so the llm gives up first and says why
	speakTimeout = 45 * time.Second
	// a reply still playing after this is cut off rather than holding up the next wake
	longestSpeech = 2 * time.Minute
)

// says text in the voice channel in the guild personality's voice, returning
// once it's been said with when it started playing, false if it never did
func speak(services *state.GlobalServices, guild snowflake.ID, text string) (time.Time, bool) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), speakTimeout)
	clip, err := services.Brain.Speak(ctx, guild, text)
	cancel()
	fetching := time.Since(start)
	if err != nil {
		switch status.Code(err) {
		// no [tts] in the llm config, or the personality has no voice
		case codes.Unimplemented, codes.FailedPrecondition:
			slog.Debug("not speaking reply", slog.String("guild_id", guild.String()), slog.Any("error", err))
		default:
			slog.Error("speaking reply", slog.String("guild_id", guild.String()), slog.Any("error", err))
		}
		return time.Time{}, false
	}

	guildServices, ok := services.GetGuild(guild)
	if !ok {
		return time.Time{}, false
	}

	// ffmpeg decodes the clip from a file, the same way songs are played
	file, err := os.CreateTemp("", "speech-*")
	if err != nil {
		slog.Error("saving speech clip", slog.Any("error", err))
		return time.Time{}, false
	}
	defer os.Remove(file.Name())
	_, err = file.Write(clip)
	file.Close()
	if err != nil {
		slog.Error("saving speech clip", slog.Any("error", err))
		return time.Time{}, false
	}

	pipe, err := saudio.WavToPcmStream(file.Name())
	if err != nil {
		slog.Error("starting ffmpeg for speech", slog.Any("error", err))
		return time.Time{}, false
	}
	defer pipe.Close()

	layer := saudio.NewMixingLayer(999, 0.5, services.Config.Voice.LayerBufferFrames, pipe.Reader, saudio.RawGainEditor(2))
	if err := guildServices.Mixer.Register(speechLayer, layer); err != nil {
		slog.Error("registering speech layer", slog.Any("error", err))
		return time.Time{}, false
	}
	started := time.Now()

	select {
	case <-layer.Ctx.Done():
	case <-time.After(longestSpeech):
		guildServices.Mixer.Unregister(speechLayer)
	}

	slog.Debug("spoke reply",
		slog.String("guild_id", guild.String()),
		slog.Duration("fetching", fetching),
		slog.Duration("decoding", started.Sub(start)-fetching),
		slog.Duration("playing", time.Since(started)),
	)
	return started, true
}
