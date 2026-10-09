package events

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/snowflake/v2"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/audio"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/network"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/state"
)

const (
	// above the llm's own stt.timeout, so the llm gives up first and says why
	transcribeTimeout = 45 * time.Second
	// how often the person who woke the bot is checked for having stopped talking
	quietPoll = 50 * time.Millisecond
	// silence put between someone's utterances when they're joined, 300 ms at 16 kHz
	turnGap = 16000 * 3 / 10
)

// one transcribed utterance
type line struct {
	user snowflake.ID
	at   time.Time
	text string
}

// answers a guild's wake words one at a time: transcribes the command and the
// conversation before it, adds them to the voice channel's history, then
// replies in the voice channel's text chat and out loud
func WakeHandler(client *bot.Client, services *state.GlobalServices) func(snowflake.ID, *audio.OpusListener) {
	return func(guild snowflake.ID, listener *audio.OpusListener) {
		// audio before this was already transcribed for an earlier wake
		var heard time.Time
		for wake := range listener.Wakes() {
			heard = answerWake(client, services, guild, listener, wake, heard)
		}
	}
}

// returns how far the guild's audio has now been transcribed
func answerWake(client *bot.Client, services *state.GlobalServices, guild snowflake.ID, listener *audio.OpusListener, wake audio.Wake, heard time.Time) time.Time {
	listen := services.Config.Listen
	picked := time.Now()
	until := awaitQuiet(listener, wake, listen.CommandQuiet, listen.CommandMax)

	conn := client.VoiceManager.GetConn(guild)
	if conn == nil || conn.ChannelID() == nil {
		return until
	}
	// a voice channel's text chat shares its id
	channel := *conn.ChannelID()

	from := wake.At.Add(-listen.Context)
	if from.Before(heard) {
		from = heard
	}
	utterances := joinTurns(listener.Utterances(from, until))

	start := time.Now()
	lines := transcribe(services.Brain, utterances, listen.MinUtterance)
	transcribing := time.Since(start)
	if len(lines) == 0 {
		slog.Info("wake word with nothing transcribed", slog.String("guild_id", guild.String()), slog.Duration("transcribing", transcribing))
		return until
	}

	start = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), rememberTimeout)
	defer cancel()
	names := voiceNames(client, guild, lines)
	for _, line := range lines {
		err := services.Brain.Remember(ctx, guild, channel, names[line.user], line.text, line.at)
		if err != nil {
			slog.Error("storing voice line in llm history", slog.String("channel_id", channel.String()), slog.Any("error", err))
		}
	}
	remembering := time.Since(start)

	start = time.Now()
	reply := respond(client, services.Brain, guild, channel, 0, wake.User)
	replying := time.Since(start)

	// when the bot started talking back, or posted its reply if it didn't speak
	answered := time.Now()
	if reply != "" {
		if started, ok := speak(services, guild, reply); ok {
			answered = started
		}
	}

	// queued is the wake waiting behind an earlier one, waiting is from the
	// person going quiet to hearing the answer
	slog.Info("voice command answered",
		slog.String("guild_id", guild.String()),
		slog.Int("utterances", len(utterances)),
		slog.Int("lines", len(lines)),
		slog.Duration("queued", picked.Sub(wake.At)),
		slog.Duration("listening", until.Sub(picked)),
		slog.Duration("transcribing", transcribing),
		slog.Duration("remembering", remembering),
		slog.Duration("replying", replying),
		slog.Duration("waiting", answered.Sub(until)),
	)
	return until
}

// waits for the person who woke the bot to go quiet, at most longest after the
// wake. quiet is judged by loudness, discord keeps sending packets through
// background noise
func awaitQuiet(listener *audio.OpusListener, wake audio.Wake, quiet time.Duration, longest time.Duration) time.Time {
	ticker := time.NewTicker(quietPoll)
	defer ticker.Stop()

	deadline := wake.At.Add(longest)
	for now := range ticker.C {
		last, _ := listener.LastLoud(wake.User)
		if now.Sub(last) >= quiet || now.After(deadline) {
			return now
		}
	}
	return time.Now()
}

// joins back to back utterances by the same person into one clip, fewer and
// longer clips transcribe better and each one is a transcription call. someone
// else speaking in between keeps them apart, so the conversation stays in order
func joinTurns(utterances []audio.Utterance) []audio.Utterance {
	var out []audio.Utterance
	for _, utterance := range utterances {
		if last := len(out) - 1; last >= 0 && out[last].User == utterance.User {
			out[last].PCM = append(out[last].PCM, make([]int16, turnGap)...)
			out[last].PCM = append(out[last].PCM, utterance.PCM...)
			continue
		}
		out = append(out, utterance)
	}
	return out
}

// transcribes every utterance long enough to be speech at once, oldest first
func transcribe(llm *network.Client, utterances []audio.Utterance, minimum time.Duration) []line {
	ctx, cancel := context.WithTimeout(context.Background(), transcribeTimeout)
	defer cancel()

	texts := make([]string, len(utterances))
	var wg sync.WaitGroup
	for i, utterance := range utterances {
		if utterance.Duration() < minimum {
			continue
		}

		wg.Go(func() {
			start := time.Now()
			text, err := llm.Transcribe(ctx, audio.EncodeWAV(utterance.PCM))
			slog.Debug("transcribe call",
				slog.String("user_id", utterance.User.String()),
				slog.Duration("audio", utterance.Duration()),
				slog.Duration("took", time.Since(start)),
			)
			if err != nil {
				slog.Error("transcribing utterance", slog.String("user_id", utterance.User.String()), slog.Any("error", err))
				return
			}
			texts[i] = strings.TrimSpace(text)
		})
	}
	wg.Wait()

	var lines []line
	for i, utterance := range utterances {
		if texts[i] != "" {
			lines = append(lines, line{user: utterance.User, at: utterance.Start, text: texts[i]})
		}
	}
	return lines
}

// names everyone who spoke the way text messages are named, see speakerName
func voiceNames(client *bot.Client, guild snowflake.ID, lines []line) map[snowflake.ID]string {
	names := make(map[snowflake.ID]string)
	for _, line := range lines {
		if _, ok := names[line.user]; ok {
			continue
		}

		member, err := client.Rest.GetMember(guild, line.user)
		if err != nil {
			slog.Warn("looking up voice speaker", slog.String("user_id", line.user.String()), slog.Any("error", err))
			names[line.user] = line.user.String()
			continue
		}

		username := member.User.Username
		display := member.EffectiveName()
		if display == username {
			names[line.user] = username
		} else {
			names[line.user] = username + " (" + display + ")"
		}
	}
	return names
}
