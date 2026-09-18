package discordbot

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/agent"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/audio"
	"github.com/Y2Kwastaken/model-citizen/llm/model"
)

const (
	defaultTranscribeSeconds = 10
	maxTranscribeSeconds     = int(listenWindow / time.Second)

	// When set, /transcribe also writes each utterance it heard to this
	// directory as 16kHz wav, so what the bot actually receives can be
	// scored offline against the wake word.
	voiceDumpVariable = "VOICE_DUMP_DIR"
)

// transcribeCommand reads back what the listener heard: the same path a wake
// word takes, minus the wake word, so the pipeline can be exercised by hand.
func transcribeCommand(brain model.LanguageModel) handler.SlashCommandHandler {
	return func(data discord.SlashCommandInteractionData, event *handler.CommandEvent) error {
		guild, ok := guildID(event)
		if !ok {
			return replyEphemeral(event, "This command only works in a server.")
		}

		if !brain.HasFeature(model.STT) {
			return replyEphemeral(event, "Transcription isn't configured -- no voice models are set up.")
		}

		if err := requireSharedVoice(event.Client(), guild, event.User().ID); err != nil {
			return replyEphemeral(event, err.Error())
		}

		listener, ok := listenerFor(guild)
		if !ok {
			return replyEphemeral(event, "I'm not listening in this server -- try /leave then /join.")
		}

		seconds := defaultTranscribeSeconds
		if given, ok := data.OptInt("seconds"); ok {
			seconds = min(max(given, 1), maxTranscribeSeconds)
		}

		if err := event.DeferCreateMessage(true); err != nil {
			return err
		}

		// Transcription is a round trip per utterance, so it runs off the gateway goroutine (see handleJoin).
		go func() {
			ctx := context.WithoutCancel(event.Ctx)
			utterances := listener.Context(time.Now(), time.Duration(seconds)*time.Second)
			if len(utterances) == 0 {
				editResponse(event, "I didn't hear anything -- every frame failed to decrypt, or nobody was speaking.")
				return
			}
			if dir := os.Getenv(voiceDumpVariable); dir != "" {
				dumpUtterances(dir, utterances)
			}

			lines := transcribeUtterances(ctx, event.Client(), guild, utterances)
			if len(lines) == 0 {
				editResponse(event, "*(nothing recognised)*")
				return
			}
			editResponse(event, agent.Truncate(transcript(lines), 2000))
		}()

		return nil
	}
}

// dumpUtterances writes what the listener heard, one file per utterance, named by when it started and who spoke.
func dumpUtterances(dir string, utterances []audio.Utterance) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Error("creating voice dump dir", slog.String("dir", dir), slog.Any("err", err))
		return
	}
	for _, u := range utterances {
		name := fmt.Sprintf("%s_%s.wav", u.Start.Format("150405.000"), u.User)
		if err := os.WriteFile(filepath.Join(dir, name), audio.EncodeWAV(u.PCM), 0o644); err != nil {
			slog.Error("writing voice dump", slog.String("file", name), slog.Any("err", err))
			continue
		}
		slog.Info("voice dump", slog.String("file", name), slog.Duration("length", u.Duration().Round(10*time.Millisecond)))
	}
}
