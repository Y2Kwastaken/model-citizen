package llm

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

// The bot's personality. Embedded rather than read from disk so it ships in the
// binary — data/ is excluded by .dockerignore and there is no path to resolve.
//
//go:embed prompts/system.md
var defaultSystemPrompt string

// a perModelTimeout
const perModelTimeout = kill_threshold

type BrainClient interface {
	// Chat sends the channel's recorded history and returns the model's reply.
	//
	// The API itself is stateless; the brain owns the history via History().
	Chat(ctx context.Context, channel snowflake.ID) (string, error)
	// the history provision for this brain
	History() HistoryProvider
}

type BasicClientProvider struct {
	models       ModelManager
	systemPrompt string
	history      HistoryProvider
}

// NewBrainClient builds a client from environment variable *names*, matching
// how Start takes its Discord token, so deployment config stays in data/.env.
func NewBrainClient(modelsFile string, modelAuthKey string, modelNameKey string, modelLinkKey string) (BrainClient, error) {
	models, err := newModelManager(modelsFile, modelAuthKey, modelNameKey, modelLinkKey)
	if err != nil {
		return nil, err
	}

	provider := BasicClientProvider{
		models:       models,
		systemPrompt: defaultSystemPrompt,
		history:      NewChatHistory(),
	}

	return &provider, nil
}

func (provider *BasicClientProvider) Chat(ctx context.Context, channel snowflake.ID) (string, error) {
	history := provider.history

	if history.Empty(channel) {
		return "", fmt.Errorf("no history to give chat model")
	}

	orderedHistory := history.OrderedHistory(channel)
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(orderedHistory)+1)
	messages = append(messages, openai.SystemMessage(provider.systemPrompt))

	for _, message := range orderedHistory {
		switch message.Who {
		case Self:
			messages = append(messages, openai.AssistantMessage(message.Content))
		default:
			messages = append(messages, openai.UserMessage(message.Name+": "+stripSelfMentions(message.Content, orderedHistory)))
		}
	}

	return draw(ctx, provider.complete, openai.ChatCompletionNewParams{
		Messages:        messages,
		ReasoningEffort: shared.ReasoningEffortNone,
		Temperature:     openai.Float(temperature),
		TopP:            openai.Float(topP),
		MaxTokens:       openai.Int(maxReplyTokens),
	}, orderedHistory)
}

// complete runs one completion against the currently selected model, times it, and hands the verdict to the manager.
func (provider *BasicClientProvider) complete(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	var lastErr error

	tried := make(map[int]bool, provider.models.Count())

	for attempt := range provider.models.Count() {
		model := provider.models.Model()

		// every model is benched we must parole
		if tried[model.index] {
			break
		}
		tried[model.index] = true
		params.Model = model.name

		attemptCtx, cancel := context.WithTimeout(ctx, perModelTimeout)
		start := time.Now()
		completion, err := model.client.Chat.Completions.New(attemptCtx, params)
		latency := time.Since(start)
		cancel()

		if err == nil {
			// don't punish on errors
			provider.models.Judge(model, latency)
			return completion, nil
		}

		if ctx.Err() != nil {
			return nil, fmt.Errorf("%s: %w", model.name, err)
		}

		lastErr = fmt.Errorf("%s: %w", model.name, err)
		// logged before the bench so the lines read in the order they happened
		fields := []any{
			slog.String("model", model.name),
			slog.Int("attempt", attempt),
			slog.Duration("latency", latency),
			slog.Any("error", err),
		}

		// a report the why
		if apiErr, ok := errors.AsType[*openai.Error](err); ok {
			fields = append(fields, slog.Int("status", apiErr.StatusCode))
		}
		slog.Warn("model call failed, failing over", fields...)
		provider.models.Fail(model, err)
	}

	if lastErr == nil {
		return nil, fmt.Errorf("no models in rotation")
	}
	return nil, fmt.Errorf("every model failed: %w", lastErr)
}

func (provider *BasicClientProvider) History() HistoryProvider {
	return provider.history
}
