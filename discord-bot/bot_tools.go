package discordbot

import (
	"context"
	"encoding/json/v2"
	"log/slog"

	"github.com/disgoorg/disgo/bot"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/music"
	"github.com/Y2Kwastaken/model-citizen/llm"
)

func registerTools(client *bot.Client, brain llm.BrainClient) {
	tools := brain.Tools()
	tools.Register(playSong(client))
	tools.Register(skipSong(client))
	tools.Register(rewindSong(client))
	tools.Register(joinVoiceTool(client))
	tools.Register(leaveVoiceTool(client))
}

func playSong(client *bot.Client) llm.Tool {
	return llm.Tool{
		Definition: shared.FunctionDefinitionParam{
			Name:        "play",
			Description: openai.String("queue a song and start playback, joining the caller's voice channel first if not already in one"),
			Parameters: shared.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"song": map[string]any{"type": "string", "description": "a link or search terms"},
				},
				"required": []string{"song"},
			},
		},
		Handle: func(ctx context.Context, call llm.Invocation) string {
			var args struct {
				Song string `json:"song"`
			}
			// the model does not always write valid json
			if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
				return "could not read the song argument: " + err.Error()
			}

			queued, err := enqueue(ctx, client, call.Guild, call.Caller, args.Song)
			if err != nil {
				return err.Error()
			}

			if queued.player.Playing() {
				return "queued " + queued.label + ", it plays after the current song"
			}

			startPlaybackAsync(ctx, call.Guild, queued, func(song music.Song, err error) {
				if err == nil {
					slog.Info("model started playback",
						slog.String("guild_id", call.Guild.String()),
						slog.String("song", playbackLabel(song)),
					)
				}
			})
			return "queued " + queued.label + ", starting playback now"
		},
	}
}

// skipSong is /skip for the model. It takes no arguments.
func skipSong(client *bot.Client) llm.Tool {
	return llm.Tool{
		Definition: shared.FunctionDefinitionParam{
			Name:        "skip",
			Description: openai.String("skip the song currently playing in the voice channel"),
		},
		Handle: func(_ context.Context, call llm.Invocation) string {
			song, err := skipCurrent(client, call.Guild, call.Caller)
			if err != nil {
				return err.Error()
			}
			return "skipped " + playbackLabel(song)
		},
	}
}

// rewindSong is /rewind for the model. It takes no arguments.
func rewindSong(client *bot.Client) llm.Tool {
	return llm.Tool{
		Definition: shared.FunctionDefinitionParam{
			Name:        "rewind",
			Description: openai.String("restart the song currently playing from the beginning"),
		},
		Handle: func(_ context.Context, call llm.Invocation) string {
			song, err := rewindCurrent(client, call.Guild, call.Caller)
			if err != nil {
				return err.Error()
			}
			return "replaying " + playbackLabel(song)
		},
	}
}

// joinVoiceTool is /join for the model. It takes no arguments.
func joinVoiceTool(client *bot.Client) llm.Tool {
	return llm.Tool{
		Definition: shared.FunctionDefinitionParam{
			Name:        "join",
			Description: openai.String("join the voice channel the caller is in"),
		},
		Handle: func(ctx context.Context, call llm.Invocation) string {
			if err := joinCaller(ctx, client, call.Guild, call.Caller); err != nil {
				return err.Error()
			}
			return "joined the voice channel"
		},
	}
}

// leaveVoiceTool is /leave for the model. It takes no arguments.
func leaveVoiceTool(client *bot.Client) llm.Tool {
	return llm.Tool{
		Definition: shared.FunctionDefinitionParam{
			Name:        "leave",
			Description: openai.String("leave the voice channel, dropping the queue"),
		},
		Handle: func(ctx context.Context, call llm.Invocation) string {
			if err := leaveVoice(ctx, client, call.Guild, call.Caller); err != nil {
				return err.Error()
			}
			return "left the voice channel"
		},
	}
}
