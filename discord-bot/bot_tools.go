package discordbot

import (
	"context"
	"encoding/json/v2"
	"strings"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	"github.com/Y2Kwastaken/model-citizen/llm/model"
)

func registerTools(client *bot.Client, brain model.LanguageModel) {
	tools := brain.Tools()
	tools.Register(playSong(client))
	tools.Register(skipSong(client))
	tools.Register(rewindSong(client))
	tools.Register(joinVoiceTool(client))
	tools.Register(leaveVoiceTool(client))
	tools.Register(disconnectUser(client))
}

// disconnectUser kicks a named user out of the bot's voice channel for the model.
func disconnectUser(client *bot.Client) model.Tool {
	return model.Tool{
		Definition: shared.FunctionDefinitionParam{
			Name:        "disconnect",
			Description: openai.String("disconnect a user from the voice channel. use for: kick X, disconnect X, remove X"),
			Parameters: shared.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"user": map[string]any{"type": "string", "description": "the user's name, nickname or username"},
				},
				"required": []string{"user"},
			},
		},
		Handle: func(_ context.Context, call model.Invocation) string {
			var args struct {
				User string `json:"user"`
			}
			if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
				return "could not read the user argument: " + err.Error()
			}

			if err := requireSharedVoice(client, call.Guild, call.Caller); err != nil {
				return err.Error()
			}
			channel, _ := botVoiceChannel(client, call.Guild)

			var users []discord.User
			for _, user := range findApproximateUsersInVoice(client, call.Guild, channel, args.User) {
				if user.ID != client.ID() {
					users = append(users, user)
				}
			}
			switch len(users) {
			case 0:
				return "no one called " + args.User + " is in the voice channel"
			case 1:
				if err := disconnectFromVoice(client, call.Guild, users[0].ID); err != nil {
					return "couldn't disconnect " + users[0].EffectiveName() + ": " + err.Error()
				}
				return "disconnected " + users[0].EffectiveName()
			}

			names := make([]string, len(users))
			for i, user := range users {
				names[i] = user.EffectiveName() + " (" + user.Username + ")"
			}
			return "several people match, ask which one: " + strings.Join(names, ", ")
		},
	}
}

// playSong is /play for the model. It answers once the song is queued.
func playSong(client *bot.Client) model.Tool {
	return model.Tool{
		Definition: shared.FunctionDefinitionParam{
			Name:        "play",
			Description: openai.String("queue a song and start playback, joining the caller's voice channel first if not already in one. use for: play X, put on X, queue X"),
			Parameters: shared.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"song": map[string]any{"type": "string", "description": "a link or search terms"},
				},
				"required": []string{"song"},
			},
		},
		Handle: func(ctx context.Context, call model.Invocation) string {
			var args struct {
				Song string `json:"song"`
			}
			// the model does not always write valid json
			if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
				return "could not read the song argument: " + err.Error()
			}

			queued, started, err := playQueued(ctx, client, call.Guild, call.Caller, args.Song)
			if err != nil {
				return err.Error()
			}
			if !started {
				return "queued " + queued.label + ", it plays after the current song"
			}
			return "queued " + queued.label + ", starting playback now"
		},
	}
}

// skipSong is /skip for the model. It takes no arguments.
func skipSong(client *bot.Client) model.Tool {
	return model.Tool{
		Definition: shared.FunctionDefinitionParam{
			Name:        "skip",
			Description: openai.String("skip the song currently playing in the voice channel. use for: skip, next song, play the next one, I don't like this one"),
		},
		Handle: func(_ context.Context, call model.Invocation) string {
			song, err := skipCurrent(client, call.Guild, call.Caller)
			if err != nil {
				return err.Error()
			}
			return "skipped " + playbackLabel(song)
		},
	}
}

// rewindSong is /rewind for the model. It takes no arguments.
func rewindSong(client *bot.Client) model.Tool {
	return model.Tool{
		Definition: shared.FunctionDefinitionParam{
			Name:        "rewind",
			Description: openai.String("restart the song currently playing from the beginning. use for: rewind, start over, play it again, run it back"),
		},
		Handle: func(_ context.Context, call model.Invocation) string {
			song, err := rewindCurrent(client, call.Guild, call.Caller)
			if err != nil {
				return err.Error()
			}
			return "replaying " + playbackLabel(song)
		},
	}
}

// joinVoiceTool is /join for the model. It takes no arguments.
func joinVoiceTool(client *bot.Client) model.Tool {
	return model.Tool{
		Definition: shared.FunctionDefinitionParam{
			Name:        "join",
			Description: openai.String("join the voice channel the caller is in. use for: join, come to voice, get in here"),
		},
		Handle: func(ctx context.Context, call model.Invocation) string {
			if err := joinCaller(ctx, client, call.Guild, call.Caller); err != nil {
				return err.Error()
			}
			return "joined the voice channel"
		},
	}
}

// leaveVoiceTool is /leave for the model. It takes no arguments.
func leaveVoiceTool(client *bot.Client) model.Tool {
	return model.Tool{
		Definition: shared.FunctionDefinitionParam{
			Name:        "leave",
			Description: openai.String("leave the voice channel, dropping the queue. use for: leave, disconnect, go away, stop the music"),
		},
		Handle: func(ctx context.Context, call model.Invocation) string {
			if err := leaveVoice(ctx, client, call.Guild, call.Caller); err != nil {
				return err.Error()
			}
			return "left the voice channel"
		},
	}
}
