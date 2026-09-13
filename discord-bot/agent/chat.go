package agent

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/Y2Kwastaken/model-citizen/llm"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
)

// mentionPattern matches a user or role mention, including the legacy "!" form
// Discord still sends for nicknamed members.
var mentionPattern = regexp.MustCompile(`<@[!&]?\d+>`)

func HandleMessage(brain llm.BrainClient, event *events.GuildMessageCreate) {
	message := event.Message

	appendHistory(brain, message, event.Client().ID())
	if !shouldRespond(event.Client().ID(), message) {
		return
	}

	go respond(event.Client(), brain, message.ChannelID, event.MessageID)
}

func respond(client *bot.Client, brain llm.BrainClient, channel snowflake.ID, messageID snowflake.ID) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.Rest.SendTyping(channel); err != nil {
		slog.Warn("Failed to send typing indicator",
			slog.String("channel_id", channel.String()),
			slog.Any("error", err),
		)
	}

	reply, err := brain.Chat(ctx, channel)
	if err != nil {
		slog.Error("Bot Reply Failure (Timeout Likely)", slog.String("channel_id", channel.String()), slog.Any("error", err))
		return
	}

	reply = strings.TrimSpace(reply)
	if reply == "" {
		slog.Error("Empty Bot Text reply", slog.String("channel_id", channel.String()))
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

func appendHistory(brain llm.BrainClient, message discord.Message, selfID snowflake.ID) {
	author := message.Author
	var sender llm.Sender
	if author.Bot && selfID == message.Author.ID {
		sender = llm.Self
	} else {
		sender = llm.User
	}

	// the model has no use for "<@1234567890>" and would parrot it back
	content := stripMentions(message.Content)
	if content == "" {
		return
	}

	brain.History().InsertMessage(
		message.ChannelID,
		sender,
		message.Author.Username,
		content,
	)
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

// stripMentions removes mention tokens and normalises the remaining whitespace.
func stripMentions(content string) string {
	return strings.Join(strings.Fields(mentionPattern.ReplaceAllString(content, " ")), " ")
}

func truncate(content string, limit int) string {
	runes := []rune(content)
	if len(runes) <= limit {
		return content
	}
	return string(runes[:limit-1]) + "…"
}
