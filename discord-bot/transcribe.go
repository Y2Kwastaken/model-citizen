package discordbot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/audio"
	"github.com/Y2Kwastaken/model-citizen/llm/model"
)

const (
	// What we send. The Whisper-family models are trained on 16kHz mono, so
	// everything above that is upload the model resamples away.
	captureSampleRate = 16000
	captureLanguage   = "en"

	defaultCaptureSeconds = 8
	maxCaptureSeconds     = 30

	transcribeTimeout = 30 * time.Second

	// disgo never gates the receive path on godave.Session.Ready, so the first
	// packets read after a receiver is installed are decrypted against an MLS
	// epoch that may still be settling, and against an SSRC map that Discord
	// only fills from Speaking events. Both resolve on their own; draining the
	// window here spends the failures before the user has started talking
	// rather than on their opening words.
	daveWarmup = 1500 * time.Millisecond
)

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

		seconds := defaultCaptureSeconds
		if given, ok := data.OptInt("seconds"); ok {
			seconds = min(max(given, 1), maxCaptureSeconds)
		}

		if err := event.DeferCreateMessage(true); err != nil {
			return err
		}

		// The capture blocks for the whole window, so it cannot run on the gateway
		// goroutine (see handleJoin).
		go func() {
			ctx := context.WithoutCancel(event.Ctx)
			window := time.Duration(seconds) * time.Second

			announce := func() {
				editResponse(event, fmt.Sprintf("Listening for %ds -- speak now.", seconds))
			}

			result, err := captureAndTranscribe(ctx, event.Client(), brain, guild, event.User().ID, window, announce)
			if err != nil {
				editResponse(event, err.Error())
				return
			}
			editResponse(event, result)
		}()

		return nil
	}
}

// captureAndTranscribe records user for window, hands the audio to the brain
// and returns the transcript with the numbers this POC exists to produce. The
// error, when there is one, is worded for whoever asked.
func captureAndTranscribe(ctx context.Context, client *bot.Client, brain model.LanguageModel, guild snowflake.ID, user snowflake.ID, window time.Duration, announce func()) (string, error) {
	conn := client.VoiceManager.GetConn(guild)
	if conn == nil || conn.ChannelID() == nil {
		return "", errors.New("I've lost my voice connection -- try /leave then /join.")
	}

	rec, err := audio.NewRecorder(user, window)
	if err != nil {
		slog.Error("creating recorder", slog.Any("err", err))
		return "", errors.New("Couldn't start recording.")
	}

	// This displaces whatever receiver the connection already had. Nothing else
	// sets one today; when something does, this has to put the old one back.
	conn.SetOpusFrameReceiver(rec)

	// Let the decrypt failures drain before the capture window opens; see
	// daveWarmup.
	select {
	case <-time.After(daveWarmup):
	case <-ctx.Done():
		return "", errors.New("Recording was cancelled.")
	}

	rec.Start()
	announce()

	select {
	case <-time.After(window):
	case <-ctx.Done():
	}

	pcm, frames := rec.Stop()
	if frames == 0 {
		return "", errors.New("I didn't hear anything -- every frame failed to decrypt, or you weren't speaking. " +
			"Try going quiet for a second and then talking, so Discord sends a fresh Speaking event.")
	}

	flac, err := audio.EncodeFLAC(pcm, captureSampleRate)
	if err != nil {
		slog.Error("encoding voice capture", slog.Any("err", err))
		return "", errors.New("Couldn't encode the recording -- check the logs.")
	}

	ctx, cancel := context.WithTimeout(ctx, transcribeTimeout)
	defer cancel()

	start := time.Now()
	text, err := brain.Transcribe(ctx, model.Clip{Data: flac, Format: "flac", Language: captureLanguage})
	roundTrip := time.Since(start)

	if err != nil {
		slog.Error("transcribing voice capture", slog.Any("err", err))
		return "", errors.New("Transcription failed -- check the logs.")
	}

	captured := float64(len(pcm)) / audio.SampleRate

	slog.Info("transcribed voice capture",
		slog.String("guild_id", guild.String()),
		slog.String("user_id", user.String()),
		slog.Int("frames", frames),
		slog.Float64("audio_seconds", captured),
		slog.Int("upload_bytes", len(flac)),
		slog.Duration("round_trip", roundTrip),
		// Logged last so it is the tail of the line: while this is a POC the
		// transcript is the result, and reading it should not mean digging it
		// out from between the measurements.
		slog.String("text", text),
	)

	if text == "" {
		text = "*(nothing recognised)*"
	}

	// The POC is a measurement, so the numbers go where they can be read
	// without tailing the container.
	return fmt.Sprintf("%s\n\n-# %d packets | %.1fs audio | %dkB uploaded | %dms round trip",
		text, frames, captured, len(flac)/1000, roundTrip.Milliseconds()), nil
}
