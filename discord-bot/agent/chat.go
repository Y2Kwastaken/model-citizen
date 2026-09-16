package agent

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/Y2Kwastaken/model-citizen/llm/model"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
)

var mentionPattern = regexp.MustCompile(`<@[!&]?\d+>`)

func HandleMessage(brain model.LanguageModel, event *events.GuildMessageCreate) {
	message := event.Message

	appendHistory(brain, message, event.Client().ID())
	if !shouldRespond(event.Client().ID(), message) {
		return
	}

	go respond(event.Client(), brain, model.Origin{
		Guild:   event.GuildID,
		Channel: message.ChannelID,
		Caller:  message.Author.ID,
	}, event.MessageID)
}

func respond(client *bot.Client, brain model.LanguageModel, origin model.Origin, messageID snowflake.ID) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	channel := origin.Channel

	// best effort and off the critical path: overlaps the discord round trip with the model call
	go func() {
		if err := client.Rest.SendTyping(channel); err != nil {
			slog.Warn("Failed to send typing indicator",
				slog.String("channel_id", channel.String()),
				slog.Any("error", err),
			)
		}
	}()

	reply, err := brain.Chat(ctx, origin)
	if err != nil {
		slog.Error("Bot Reply Failure (Timeout Likely)", slog.String("channel_id", channel.String()), slog.Any("error", err))
		return
	}

	reply = strings.TrimSpace(reply)
	if reply == "" {
		slog.Warn("Empty Bot Text reply", slog.String("channel_id", channel.String()))
		return
	}

	_, err = client.Rest.CreateMessage(channel, discord.NewMessageCreate().
		WithContent(truncate(reply, 2000)).
		WithMessageReferenceByID(messageID).
		WithAllowedMentions(&discord.AllowedMentions{
			Parse:       []discord.AllowedMentionType{},
			RepliedUser: true,
		}))

	if err != nil {
		slog.Error("error sending message", slog.String("channel_id", channel.String()), slog.Any("error", err))
	} else {
		slog.Info("sent message", slog.String("channel_id", channel.String()), slog.String("message", reply))
	}

}

func appendHistory(brain model.LanguageModel, message discord.Message, selfID snowflake.ID) {
	author := message.Author
	var sender model.Sender
	if author.Bot && selfID == message.Author.ID {
		sender = model.Self
	} else {
		sender = model.User
	}

	// the model has no use for "<@1234567890>", but it does care who was addressed
	content := inlineMentions(message)
	if content == "" {
		return
	}

	brain.History().InsertMessage(
		message.ChannelID,
		sender,
		speakerName(message),
		content,
	)
}

// speakerName is the username, plus the name people in the room actually call
// them if it differs: the guild nickname, else the global display name.
// "jimbo1230054 (Jimbotron)" lets the model connect what it is told to who said it.
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

func shouldRespond(bot snowflake.ID, message discord.Message) bool {
	if message.MentionEveryone || message.Author.Bot {
		return false
	}

	mentionBot := false
	for _, user := range message.Mentions {
		if user.ID == bot {
			mentionBot = true
			break
		}
	}

	return mentionBot
}

// inlineMentions rewrites user mention tokens to "@username" so the model sees
// who was addressed without any discord ids. Anything left unresolved (roles,
// users not in the payload) is dropped, and whitespace is normalised.
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

func truncate(content string, limit int) string {
	runes := []rune(content)
	if len(runes) <= limit {
		return content
	}
	return string(runes[:limit-1]) + "…"
}
