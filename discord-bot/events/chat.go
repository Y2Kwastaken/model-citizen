package events

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/network"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/state"
)

const (
	rememberTimeout = 5 * time.Second
	replyTimeout    = 45 * time.Second
	messageLimit    = 2000
)

var mentionPattern = regexp.MustCompile(`<@[!&]?\d+>`)

func ChatListener(services *state.GlobalServices) bot.EventListener {
	return bot.NewListenerFunc(func(e *events.GuildMessageCreate) {
		message := e.Message
		if message.Author.Bot {
			return
		}

		content := inlineMentions(message)
		if content == "" {
			return
		}

		// off the gateway, remember runs before reply so the reply sees the message
		go handle(e.Client(), services.Brain, e.GuildID, message, content)
	})
}

func handle(client *bot.Client, llm *network.Client, guild snowflake.ID, message discord.Message, content string) {
	channel := message.ChannelID

	ctx, cancel := context.WithTimeout(context.Background(), rememberTimeout)
	err := llm.Remember(ctx, guild, channel, speakerName(message), content, message.CreatedAt)
	cancel()
	if err != nil {
		slog.Error("storing message in llm history", slog.String("channel_id", channel.String()), slog.Any("error", err))
	}

	if !shouldRespond(client.ID(), message) {
		return
	}
	respond(client, llm, guild, channel, message.ID, message.Author.ID)
}

// posts the llm's reply and returns it, empty when nothing was posted
func respond(client *bot.Client, llm *network.Client, guild, channel, messageID, user snowflake.ID) string {
	ctx, cancel := context.WithTimeout(context.Background(), replyTimeout)
	defer cancel()

	// best effort, overlaps with the model call
	go func() {
		if err := client.Rest.SendTyping(channel); err != nil {
			slog.Warn("sending typing indicator", slog.String("channel_id", channel.String()), slog.Any("error", err))
		}
	}()

	start := time.Now()
	reply, err := llm.Reply(ctx, guild, channel, user)
	slog.Debug("reply call", slog.String("channel_id", channel.String()), slog.Duration("took", time.Since(start)))
	if err != nil {
		slog.Error("llm reply", slog.String("channel_id", channel.String()), slog.Any("error", err))
		switch status.Code(err) {
		case codes.Unavailable, codes.DeadlineExceeded:
			// don't leave a mention unanswered when the llm is down
			send(client, channel, messageID, "my brain is offline right now, try again in a bit")
		}
		return ""
	}

	reply = strings.TrimSpace(reply)
	if reply == "" {
		slog.Warn("empty llm reply", slog.String("channel_id", channel.String()))
		return ""
	}

	send(client, channel, messageID, reply)
	return reply
}

// replies to messageID only pinging the replied user, a zero messageID just posts
func send(client *bot.Client, channel, messageID snowflake.ID, content string) {
	message := discord.NewMessageCreate().
		WithContent(truncate(content, messageLimit)).
		WithAllowedMentions(&discord.AllowedMentions{
			Parse:       []discord.AllowedMentionType{},
			RepliedUser: true,
		})
	// voice commands have no message to reply to
	if messageID != 0 {
		message = message.WithMessageReferenceByID(messageID)
	}

	_, err := client.Rest.CreateMessage(channel, message)
	if err != nil {
		slog.Error("sending chat reply", slog.String("channel_id", channel.String()), slog.Any("error", err))
	}
}

// username plus nickname or display name if it differs e.g. jimbo1230054 (Jimbotron)
func speakerName(message discord.Message) string {
	username := message.Author.Username

	display := message.Author.EffectiveName()
	if message.Member != nil && message.Member.Nick != nil {
		display = *message.Member.Nick
	}

	if display == username {
		return username
	}
	return username + " (" + display + ")"
}

// direct mentions only, @everyone doesn't count
func shouldRespond(self snowflake.ID, message discord.Message) bool {
	if message.MentionEveryone {
		return false
	}

	for _, user := range message.Mentions {
		if user.ID == self {
			return true
		}
	}
	return false
}

// rewrites mentions to @username so the model sees who was addressed
// unresolved mentions (roles, missing users) are dropped
func inlineMentions(message discord.Message) string {
	pairs := make([]string, 0, len(message.Mentions)*4)
	for _, user := range message.Mentions {
		id := user.ID.String()
		name := "@" + user.Username
		pairs = append(pairs, "<@"+id+">", name, "<@!"+id+">", name)
	}

	content := strings.NewReplacer(pairs...).Replace(message.Content)
	content = mentionPattern.ReplaceAllString(content, " ")
	return strings.Join(strings.Fields(content), " ")
}

// cuts content to limit runes ending in an ellipsis
func truncate(content string, limit int) string {
	runes := []rune(content)
	if len(runes) <= limit {
		return content
	}
	return string(runes[:limit-1]) + "…"
}
