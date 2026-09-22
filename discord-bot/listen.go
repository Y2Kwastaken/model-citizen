package discordbot

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/agent"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/audio"
	"github.com/Y2Kwastaken/model-citizen/llm/model"
)

const (
	// retained voice length per speaker
	listenWindow = 30 * time.Second
	// a wake has this much context
	contextWindow = 10 * time.Second
	// how long of quiet before processing is hit
	commandQuiet = 350 * time.Millisecond
	// how long can be spent on speaking
	commandMax = 10 * time.Second
	// no "incoherence"
	minUtterance      = 400 * time.Millisecond
	wakeDebounce      = 3 * time.Second
	captureLanguage   = "en"
	transcribeTimeout = 30 * time.Second
	// synthesis plus playback of one reply
	speakTimeout = 60 * time.Second
)

// ears is the per-guild listening state. The brain and wake word are set
// once by Start; listeners come and go with voice connections.
var ears struct {
	brain     model.LanguageModel
	wake      *audio.WakeWord
	threshold float32

	mu      sync.Mutex
	byGuild map[snowflake.ID]*audio.Listener
}

// line is one transcribed utterance.
type line struct {
	At   time.Time
	Name string
	Text string
}

// listen installs a Listener on a freshly opened connection and answers its wake word triggers.
func listen(client *bot.Client, guild snowflake.ID, channel snowflake.ID, conn voice.Conn) {
	listener := audio.NewListener(ears.wake, listenWindow, ears.threshold, wakeDebounce)
	conn.SetOpusFrameReceiver(listener)

	ears.mu.Lock()
	if ears.byGuild == nil {
		ears.byGuild = make(map[snowflake.ID]*audio.Listener)
	}
	ears.byGuild[guild] = listener
	ears.mu.Unlock()

	if ears.wake == nil {
		return
	}
	go func() {
		// the channel closes when the connection does
		for trigger := range listener.Triggers() {
			handleWake(client, guild, channel, listener, trigger, false)
		}
	}()
}

// stops listening for guild's listener
func stopListening(guild snowflake.ID) {
	ears.mu.Lock()
	defer ears.mu.Unlock()
	if listener, ok := ears.byGuild[guild]; ok {
		listener.Close()
		delete(ears.byGuild, guild)
	}
}

func listenerFor(guild snowflake.ID) (*audio.Listener, bool) {
	ears.mu.Lock()
	defer ears.mu.Unlock()
	listener, ok := ears.byGuild[guild]
	return listener, ok
}

// handleWake transcribes what everyone said leading up to and including the
// command after the wake word, and has the brain answer out loud in the
// voice channel and in its chat.
//
// Latency is the whole game here. Everything said before the wake word is
// final the moment it fires, so it goes to the transcriber while the speaker
// is still talking; only the command itself waits for them to finish.
func handleWake(client *bot.Client, guild snowflake.ID, channel snowflake.ID, listener *audio.Listener, trigger audio.Trigger, chat bool) {
	if trigger.User != snowflake.ID(0) {
		slog.Info("wake word",
			slog.String("guild_id", guild.String()),
			slog.String("user_id", trigger.User.String()),
			slog.Float64("score", float64(trigger.Score)),
		)
	}

	// we have 90 seconds to react
	ctx, cancel := context.WithTimeout(context.Background(), commandMax+transcribeTimeout+30*time.Second)
	defer cancel()

	// send a typing indicator
	go func() {
		if err := client.Rest.SendTyping(channel); err != nil {
			slog.Debug("typing indicator", slog.Any("err", err))
		}
	}()

	early := closedBefore(listener.Context(trigger.At, contextWindow), trigger.At)
	earlyLines := make(chan []line, 1)
	go func() { earlyLines <- transcribeUtterances(ctx, client, guild, early) }()

	finished := awaitQuiet(ctx, listener, trigger.User, trigger.At)
	late := excluding(listener.Context(finished, contextWindow+finished.Sub(trigger.At)), early)
	lines := append(<-earlyLines, transcribeUtterances(ctx, client, guild, late)...)
	slices.SortFunc(lines, func(a, b line) int { return a.At.Compare(b.At) })
	transcribed := time.Now()

	if len(lines) == 0 {
		slog.Info("wake word with nothing to transcribe", slog.String("guild_id", guild.String()))
		return
	}

	// The voice channel is the conversation. Anyone typing in its chat lands
	// in the same history through HandleMessage, and so does our reply.
	history := ears.brain.History()
	for _, l := range lines {
		history.InsertMessage(channel, model.User, l.Name, "["+l.At.Format("15:04:05")+"] "+l.Text)
	}

	reply, err := ears.brain.Chat(ctx, model.Origin{Guild: guild, Channel: channel, Caller: trigger.User})
	if err != nil {
		slog.Error("answering wake word", slog.String("guild_id", guild.String()), slog.Any("err", err))
		return
	}
	answered := time.Now()
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return
	}

	// The text goes up whatever happens to the voice: it is the fallback
	// when there is no voice to speak with, and the record when there is.
	// Synthesis starts at the same time, so the line follows the message
	// as closely as the voice allows.
	type spoken struct {
		synthesized time.Time // when the clip was ready to play
		err         error
	}
	said := make(chan spoken, 1)
	if ears.brain.HasFeature(model.TTS) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), speakTimeout)
			defer cancel()
			pcm, err := synthesize(ctx, ears.brain, reply)
			ready := time.Now()
			if err == nil {
				err = play(ctx, client, guild, pcm)
			}
			said <- spoken{ready, err}
		}()
	} else {
		said <- spoken{answered, nil}
	}

	if chat {
		if _, err := client.Rest.CreateMessage(channel, discord.NewMessageCreate().
			WithContent(agent.Truncate(reply, 2000)).
			WithAllowedMentions(&discord.AllowedMentions{Parse: []discord.AllowedMentionType{}})); err != nil {
			slog.Error("replying in voice chat", slog.String("channel_id", channel.String()), slog.Any("err", err))
		}
	}
	posted := time.Now()

	line := <-said
	if line.err != nil {
		slog.Warn("saying the reply, text only", slog.String("guild_id", guild.String()), slog.Any("err", line.err))
	}

	// Where the time went. Up to "synth" is the wait before anything is
	// heard; "say" is the line's own length, not a delay.
	slog.Info("wake word answered",
		slog.String("guild_id", guild.String()),
		slog.Duration("command", finished.Sub(trigger.At).Round(time.Millisecond)),
		slog.Duration("transcribe", transcribed.Sub(finished).Round(time.Millisecond)),
		slog.Duration("chat", answered.Sub(transcribed).Round(time.Millisecond)),
		slog.Duration("post", posted.Sub(answered).Round(time.Millisecond)),
		slog.Duration("synth", line.synthesized.Sub(answered).Round(time.Millisecond)),
		slog.Duration("say", time.Since(line.synthesized).Round(time.Millisecond)),
		slog.Duration("heard_after", line.synthesized.Sub(trigger.At).Round(time.Millisecond)),
	)
}

// closedBefore keeps the utterances that had ended by at: the speaker had
// been quiet for a full pause. Anything still going is left for later, so it
// is not transcribed mid-word and then again.
func closedBefore(utterances []audio.Utterance, at time.Time) []audio.Utterance {
	return slices.DeleteFunc(utterances, func(u audio.Utterance) bool {
		return at.Sub(u.Start.Add(u.Duration())) < audio.UtteranceGap
	})
}

// excluding drops the utterances already in done. An utterance is the same
// one across two Context calls if it starts at the same packet.
func excluding(utterances []audio.Utterance, done []audio.Utterance) []audio.Utterance {
	type key struct {
		user  snowflake.ID
		start time.Time
	}
	seen := make(map[key]bool, len(done))
	for _, u := range done {
		seen[key{u.User, u.Start}] = true
	}
	return slices.DeleteFunc(utterances, func(u audio.Utterance) bool { return seen[key{u.User, u.Start}] })
}

// awaitQuiet returns once user has been silent for commandQuiet, or commandMax
// after since. That is when the command that followed the wake word is over.
func awaitQuiet(ctx context.Context, listener *audio.Listener, user snowflake.ID, since time.Time) time.Time {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return time.Now()
		case now := <-ticker.C:
			last, ok := listener.LastHeard(user)
			if !ok || now.Sub(last) >= commandQuiet || now.Sub(since) >= commandMax {
				return now
			}
		}
	}
}

// transcribeUtterances sends every utterance long enough to be words to the
// brain, in parallel with each other and with looking up who spoke, and
// returns the lines that came back in the order they were spoken.
func transcribeUtterances(ctx context.Context, client *bot.Client, guild snowflake.ID, utterances []audio.Utterance) []line {
	if len(utterances) == 0 {
		return nil
	}
	if !ears.brain.HasFeature(model.STT) {
		slog.Error("no transcription model for voice")
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, transcribeTimeout)
	defer cancel()

	names := make(chan map[snowflake.ID]string, 1)
	go func() { names <- speakerNames(client, guild, utterances) }()

	texts := make([]string, len(utterances))
	var wg sync.WaitGroup
	for i, u := range utterances {
		if u.Duration() < minUtterance {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			text, err := ears.brain.Transcribe(ctx, model.Clip{Data: audio.EncodeWAV(u.PCM), Format: "wav", Language: captureLanguage})
			if err != nil {
				slog.Error("transcribing utterance", slog.String("user_id", u.User.String()), slog.Any("err", err))
				return
			}
			texts[i] = text
		}()
	}
	wg.Wait()
	who := <-names

	var lines []line
	for i, u := range utterances {
		if texts[i] != "" {
			lines = append(lines, line{At: u.Start, Name: who[u.User], Text: texts[i]})
		}
	}
	slices.SortFunc(lines, func(a, b line) int { return a.At.Compare(b.At) })
	return lines
}

// speakerNames resolves who spoke, the way messages are named for the model:
// the username, plus what people in the room call them if it differs.
func speakerNames(client *bot.Client, guild snowflake.ID, utterances []audio.Utterance) map[snowflake.ID]string {
	var mu sync.Mutex
	var wg sync.WaitGroup
	names := make(map[snowflake.ID]string)
	for _, u := range utterances {
		if _, seen := names[u.User]; seen {
			continue
		}
		names[u.User] = u.User.String() // until the lookup lands
		wg.Add(1)
		go func(user snowflake.ID) {
			defer wg.Done()
			member, err := client.Rest.GetMember(guild, user)
			if err != nil {
				return
			}
			name := member.User.Username
			if display := member.EffectiveName(); display != name {
				name = fmt.Sprintf("%s (%s)", name, display)
			}
			mu.Lock()
			names[user] = name
			mu.Unlock()
		}(u.User)
	}
	wg.Wait()
	return names
}

// transcript renders lines for a person to read.
func transcript(lines []line) string {
	var b strings.Builder
	for _, l := range lines {
		fmt.Fprintf(&b, "`%s` **%s**: %s\n", l.At.Format("15:04:05"), l.Name, l.Text)
	}
	return b.String()
}
