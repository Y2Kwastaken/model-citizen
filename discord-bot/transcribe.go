package discordbot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
	"layeh.com/gopus"

	"github.com/Y2Kwastaken/model-citizen/stt"
)

const (
	mistralKeyVariable = "MISTRAL_KEY"

	// What Discord sends: 20ms of 48kHz stereo per packet.
	discordSampleRate = 48000
	discordChannels   = 2
	discordFrameSize  = discordSampleRate / 50 // samples per channel per packet

	// What we send. The Whisper-family models are trained on 16kHz mono, so
	// everything above that is upload the model resamples away.
	captureSampleRate = 16000
	decimation        = discordSampleRate / captureSampleRate

	defaultCaptureSeconds = 8
	maxCaptureSeconds     = 30

	// A timestamp gap longer than this is a reordered or bogus packet rather
	// than a pause, so one bad header cannot make us allocate minutes of
	// silence.
	maxSilenceFill = 5 * discordSampleRate

	transcribeTimeout = 30 * time.Second

	// disgo never gates the receive path on godave.Session.Ready, so the first
	// packets read after a receiver is installed are decrypted against an MLS
	// epoch that may still be settling, and against an SSRC map that Discord
	// only fills from Speaking events. Both resolve on their own; draining the
	// window here spends the failures before the user has started talking
	// rather than on their opening words.
	daveWarmup = 1500 * time.Millisecond
)

// The transcriber is built once so its connection pool outlives a single
// command; see stt.NewMistral. The error is cached too, which is what we want:
// a missing key does not start working on the second try.
var sttClient = sync.OnceValues(func() (*stt.Client, error) {
	return stt.NewMistral(mistralKeyVariable)
})

// recorder collects one user's audio off a voice connection.
//
// It implements voice.OpusFrameReceiver, which disgo drives from its own
// receive goroutine, so every field is behind the mutex.
type recorder struct {
	target  snowflake.ID
	decoder *gopus.Decoder
	limit   int

	mu       sync.Mutex
	armed    bool
	pcm      []int16
	lastTime uint32
	started  bool
	closed   bool
	frames   int
}

func newRecorder(target snowflake.ID, limit int) (*recorder, error) {
	decoder, err := gopus.NewDecoder(discordSampleRate, discordChannels)
	if err != nil {
		return nil, fmt.Errorf("creating opus decoder: %w", err)
	}

	return &recorder{
		target:  target,
		decoder: decoder,
		limit:   limit,
		pcm:     make([]int16, 0, limit),
	}, nil
}

// ReceiveOpusFrame decodes one packet into the buffer, ignoring anyone who is
// not the target user.
func (r *recorder) ReceiveOpusFrame(userID snowflake.ID, packet *voice.Packet) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.armed || r.closed || userID != r.target || len(r.pcm) >= r.limit {
		return nil
	}

	stereo, err := r.decoder.Decode(packet.Opus, discordFrameSize, false)
	if err != nil {
		// One malformed packet is not worth abandoning a capture over, and an
		// error returned here only reaches disgo's logger anyway.
		slog.Debug("decoding opus frame", slog.Any("err", err))
		return nil
	}

	// Discord clients stop transmitting through silence rather than sending
	// quiet frames, so the gap between RTP timestamps is the only record that a
	// pause happened. Left unfilled, the words butt up against each other and
	// the model hears a sentence nobody said.
	if r.started {
		if gap := int(packet.Timestamp-r.lastTime) - discordFrameSize; gap > 0 && gap < maxSilenceFill {
			r.append(make([]int16, gap))
		}
	}
	r.started = true
	r.lastTime = packet.Timestamp
	r.frames++

	// Both channels carry the same voice, so mono halves the upload and costs
	// nothing the model would have used.
	mono := make([]int16, len(stereo)/discordChannels)
	for i := range mono {
		mono[i] = int16((int32(stereo[i*discordChannels]) + int32(stereo[i*discordChannels+1])) / 2)
	}
	r.append(mono)

	return nil
}

// arm starts collection. Frames that arrive before it are decoded by nobody and
// dropped, which is the point: they are the warm-up.
func (r *recorder) arm() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.armed = true
}

// append adds samples up to the recorder's limit. The caller holds the mutex.
func (r *recorder) append(samples []int16) {
	if room := r.limit - len(r.pcm); room < len(samples) {
		samples = samples[:max(room, 0)]
	}
	r.pcm = append(r.pcm, samples...)
}

// CleanupUser is a no-op: a capture is short enough that someone leaving
// part-way through just yields a shorter clip.
func (*recorder) CleanupUser(_ snowflake.ID) {}

// Close stops collection. disgo calls it when the voice connection drops, and
// finish calls it when the window closes.
func (r *recorder) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
}

// finish ends the capture and returns the audio at captureSampleRate, with the
// number of packets that arrived.
//
// Zero packets is the failure worth reporting separately: it means the
// connection never delivered audio, which is a different problem from the user
// having said nothing.
func (r *recorder) finish() (pcm []int16, frames int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return downsample(r.pcm), r.frames
}

// downsample converts 48kHz to 16kHz by averaging each run of three samples.
//
// A box filter is a crude anti-alias and a real pipeline would want a proper
// one, but speech energy sits well below the 8kHz this leaves and the model is
// robust to what leaks through. Worth revisiting only if accuracy disappoints.
func downsample(pcm []int16) []int16 {
	out := make([]int16, 0, len(pcm)/decimation)
	for i := 0; i+decimation <= len(pcm); i += decimation {
		var sum int32
		for _, sample := range pcm[i : i+decimation] {
			sum += int32(sample)
		}
		out = append(out, int16(sum/decimation))
	}
	return out
}

func handleTranscribe(data discord.SlashCommandInteractionData, event *handler.CommandEvent) error {
	guild, ok := guildID(event)
	if !ok {
		return replyEphemeral(event, "This command only works in a server.")
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

		result, err := captureAndTranscribe(ctx, event.Client(), guild, event.User().ID, window, announce)
		if err != nil {
			editResponse(event, err.Error())
			return
		}
		editResponse(event, result)
	}()

	return nil
}

// captureAndTranscribe records user for window, sends the audio to Mistral and
// returns the transcript with the numbers this POC exists to produce. The
// error, when there is one, is worded for whoever asked.
func captureAndTranscribe(ctx context.Context, client *bot.Client, guild snowflake.ID, user snowflake.ID, window time.Duration, announce func()) (string, error) {
	transcriber, err := sttClient()
	if err != nil {
		slog.Error("building transcription client", slog.Any("err", err))
		return "", errors.New("Transcription isn't configured -- MISTRAL_KEY is missing.")
	}

	conn := client.VoiceManager.GetConn(guild)
	if conn == nil || conn.ChannelID() == nil {
		return "", errors.New("I've lost my voice connection -- try /leave then /join.")
	}

	rec, err := newRecorder(user, int(window.Seconds()*discordSampleRate))
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

	rec.arm()
	announce()

	select {
	case <-time.After(window):
	case <-ctx.Done():
	}

	pcm, frames := rec.finish()
	if frames == 0 {
		return "", errors.New("I didn't hear anything -- every frame failed to decrypt, or you weren't speaking. " +
			"Try going quiet for a second and then talking, so Discord sends a fresh Speaking event.")
	}

	audio := stt.WAV(pcm, captureSampleRate)

	ctx, cancel := context.WithTimeout(ctx, transcribeTimeout)
	defer cancel()

	start := time.Now()
	text, err := transcriber.Transcribe(ctx, audio, "en")
	roundTrip := time.Since(start)

	if err != nil {
		slog.Error("transcribing voice capture", slog.Any("err", err))
		return "", errors.New("Transcription failed -- check the logs.")
	}

	captured := float64(len(pcm)) / captureSampleRate

	slog.Info("transcribed voice capture",
		slog.String("guild_id", guild.String()),
		slog.String("user_id", user.String()),
		slog.Int("frames", frames),
		slog.Float64("audio_seconds", captured),
		slog.Int("upload_bytes", len(audio)),
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
		text, frames, captured, len(audio)/1000, roundTrip.Milliseconds()), nil
}
