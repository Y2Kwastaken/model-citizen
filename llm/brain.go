package llm

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Y2Kwastaken/model-citizen/llm/model"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

//go:embed prompts/system.md
var embeddedSystemPrompt string

const (
	perModelTimeout = 10 * time.Second
	// tool calls in one reply
	maxToolRounds = 2
)

// Config is where each rotation comes from. The fallback fields are
// environment variable names, not values, so deployment config stays in
// data/.env.
type Config struct {
	TextModelsFile  string
	VoiceModelsFile string

	// a single chat model, used when TextModelsFile is unusable
	FallbackAuthKey string
	FallbackNameKey string
	FallbackLinkKey string
}

type BrainProvider struct {
	systemPrompt string
	// a feature is supported iff it has a rotation
	rotations map[model.ModelFeature]model.ModelManager
	history   model.HistoryProvider
	tools     model.ModelTools
}

// NewBrainLanguageModel builds a rotation per feature. A rotation that cannot
// be built drops its feature rather than failing; only a brain with no
// features at all is an error.
func NewBrainLanguageModel(config Config) (model.LanguageModel, error) {
	rotations := make(map[model.ModelFeature]model.ModelManager)

	if text, err := textRotation(config); err != nil {
		slog.Warn("chat disabled", slog.Any("error", err))
	} else {
		rotations[model.Chat] = text
	}

	if voice, err := model.NewModelManager(config.VoiceModelsFile); err != nil {
		slog.Warn("transcription disabled", slog.String("file", config.VoiceModelsFile), slog.Any("error", err))
	} else {
		rotations[model.STT] = voice
	}

	if len(rotations) == 0 {
		return nil, fmt.Errorf("no model rotation could be built")
	}

	return &BrainProvider{
		systemPrompt: embeddedSystemPrompt,
		rotations:    rotations,
		history:      model.NewChatHistory(),
		tools:        model.NewModelTools(),
	}, nil
}

// textRotation reads the chat roster, falling back to the single model named
// by the environment when the file is missing or unusable.
func textRotation(config Config) (model.ModelManager, error) {
	text, err := model.NewModelManager(config.TextModelsFile)
	if err == nil {
		return text, nil
	}

	slog.Warn("falling back to the single chat model in the environment",
		slog.String("file", config.TextModelsFile),
		slog.Any("error", err),
	)
	return model.NewModelManagerFromEnvironment(config.FallbackAuthKey, config.FallbackNameKey, config.FallbackLinkKey)
}

// Implementation Basic

func (provider *BrainProvider) HasFeature(feature model.ModelFeature) bool {
	_, ok := provider.rotations[feature]
	return ok
}

func (provider *BrainProvider) Tools() model.ModelTools {
	return provider.tools
}

func (provider *BrainProvider) History() model.HistoryProvider {
	return provider.history
}

// Implementation Functions

func (provider *BrainProvider) Chat(ctx context.Context, origin model.Origin) (string, error) {
	if !provider.HasFeature(model.Chat) {
		return "", fmt.Errorf("this model does not support chatting")
	}

	history := provider.history
	channel := origin.Channel

	if history.Empty(channel) {
		return "", fmt.Errorf("no history was given to chat model")
	}

	orderedHistory := history.OrderedHistory(channel)
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(orderedHistory)+1)
	messages = append(messages, openai.SystemMessage(provider.systemPrompt))
	for _, message := range orderedHistory {
		switch message.Who {
		case model.Self:
			messages = append(messages, openai.AssistantMessage(message.Content))
		default:
			messages = append(messages, openai.UserMessage(message.Name+": "+stripSelfMentions(message.Content, orderedHistory)))
		}
	}

	return provider.doChat(ctx, openai.ChatCompletionNewParams{
		Messages:        messages,
		Tools:           provider.tools.Build(),
		ReasoningEffort: shared.ReasoningEffortNone,
		Temperature:     openai.Float(temperature),
		TopP:            openai.Float(topP),
		MaxTokens:       openai.Int(maxReplyTokens),
	}, orderedHistory, origin)
}

func (provider *BrainProvider) Transcribe(ctx context.Context, clip model.Clip) (string, error) {
	if !provider.HasFeature(model.STT) {
		return "", fmt.Errorf("this model does not support transcription")
	}

	text, err := attempt(ctx, provider.rotations[model.STT], func(ctx context.Context, selected model.Model) (string, error) {
		// a service with its own API brought its own way of being asked
		if selected.Transcribe != nil {
			return selected.Transcribe(ctx, clip)
		}

		params := openai.AudioTranscriptionNewParams{
			// a fresh reader per attempt, since a failover re-sends the clip
			File:  openai.File(bytes.NewReader(clip.Data), "clip."+clip.Format, "audio/"+clip.Format),
			Model: openai.AudioModel(selected.Name),
		}
		if clip.Language != "" {
			params.Language = openai.String(clip.Language)
		}

		transcription, err := selected.Client.Audio.Transcriptions.New(ctx, params)
		if err != nil {
			return "", err
		}
		return transcription.Text, nil
	})
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(text), nil
}

func (provider *BrainProvider) doChat(ctx context.Context, params openai.ChatCompletionNewParams, history []model.Message, origin model.Origin) (string, error) {
	for round := 0; ; round++ {
		if round >= maxToolRounds {
			params.Tools = nil
		}

		completion, err := provider.chatOnce(ctx, params)
		if err != nil {
			return "", err
		}

		if len(completion.Choices) == 0 {
			return "", fmt.Errorf("model returned no choices")
		}

		message := completion.Choices[0].Message
		if len(message.ToolCalls) == 0 || params.Tools == nil {
			params.Tools = nil
			return draw(ctx, provider.chatOnce, params, history, completion)
		}

		params.Messages = append(params.Messages, message.ToParam())
		// we call our tools here then go back for more rounds
		for _, call := range message.ToolCalls {
			result := provider.tools.CallTool(ctx, call.Function, origin)
			params.Messages = append(params.Messages, openai.ToolMessage(result, call.ID))
		}
	}
}

func (provider *BrainProvider) chatOnce(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	return attempt(ctx, provider.rotations[model.Chat], func(ctx context.Context, selected model.Model) (*openai.ChatCompletion, error) {
		params.Model = selected.Name
		return selected.Client.Chat.Completions.New(ctx, params)
	})
}

// attempt runs call against the rotation until a model answers, timing each
// try and handing the verdict to the manager. A model that errors is benched
// and the next one tried; the context's own deadline ends the loop early.
func attempt[T any](ctx context.Context, models model.ModelManager, call func(context.Context, model.Model) (T, error)) (T, error) {
	var zero T
	var lastErr error
	tried := make(map[int]bool, models.Count())

	for try := range models.Count() {
		selected := models.Model()
		if tried[selected.Index] {
			break
		}
		tried[selected.Index] = true

		attemptCtx, cancel := context.WithTimeout(ctx, perModelTimeout)
		start := time.Now()
		result, err := call(attemptCtx, selected)
		latency := time.Since(start)
		cancel()

		if err == nil {
			models.Judge(selected, latency)
			return result, nil
		}

		if ctx.Err() != nil {
			return zero, fmt.Errorf("%s: %w", selected.Name, err)
		}

		lastErr = fmt.Errorf("%s: %w", selected.Name, err)
		fields := []any{
			slog.String("model", selected.Name),
			slog.Int("attempt", try),
			slog.Duration("latency", latency),
			slog.Any("error", err),
		}
		if apiErr, ok := errors.AsType[*openai.Error](err); ok {
			fields = append(fields, slog.Int("status", apiErr.StatusCode))
		}
		slog.Warn("model call failed, failing over", fields...)
		models.Fail(selected, err)
	}

	if lastErr == nil {
		return zero, fmt.Errorf("no models in rotation")
	}
	return zero, fmt.Errorf("every model failed: %w", lastErr)
}
