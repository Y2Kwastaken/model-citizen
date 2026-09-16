package llm

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"slices"
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

type BrainProvider struct {
	systemPrompt string
	features     []model.ModelFeature
	models       model.ModelManager
	history      model.HistoryProvider
	tools        model.ModelTools
}

func NewBrainLanguageModel(modelsFile string, modelAuthKey string, modelNameKey string, modelLinkKey string) (model.LanguageModel, error) {
	models, err := model.NewModelManager(modelsFile, modelAuthKey, modelNameKey, modelLinkKey)
	if err != nil {
		return nil, err
	}

	provider := BrainProvider{
		systemPrompt: embeddedSystemPrompt,
		features:     []model.ModelFeature{model.Chat},
		models:       models,
		history:      model.NewChatHistory(),
		tools:        model.NewModelTools(),
	}

	return &provider, nil
}

// Implementation Basic

func (provider *BrainProvider) HasFeature(feature model.ModelFeature) bool {
	return slices.Contains(provider.features, feature)
}

func (provider *BrainProvider) Tools() model.ModelTools {
	return provider.tools
}

func (provider *BrainProvider) History() model.HistoryProvider {
	return provider.history
}

// Implementation Functions

func (provider *BrainProvider) Chat(ctx context.Context, origin model.Origin) (string, error) {
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

// complete runs one completion against the currently selected model, times it, and hands the verdict to the manager.
func (provider *BrainProvider) chatOnce(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	var lastErr error
	tried := make(map[int]bool, provider.models.Count())

	for attempt := range provider.models.Count() {
		selected := provider.models.Model()
		if tried[selected.Index] {
			break
		}

		tried[selected.Index] = true
		params.Model = selected.Name

		attemptCtx, cancel := context.WithTimeout(ctx, perModelTimeout)
		start := time.Now()
		completion, err := selected.Client.Chat.Completions.New(attemptCtx, params)
		latency := time.Since(start)
		cancel()

		if err == nil {
			// success path
			provider.models.Judge(selected, latency)
			return completion, nil
		}
		// failure path

		if ctx.Err() != nil {
			return nil, fmt.Errorf("%s: %w", selected.Name, err)
		}

		lastErr = fmt.Errorf("%s: %w", selected.Name, err)
		fields := []any{
			slog.String("model", selected.Name),
			slog.Int("attempt", attempt),
			slog.Duration("latency", latency),
			slog.Any("error", err),
		}

		if apiErr, ok := errors.AsType[*openai.Error](err); ok {
			fields = append(fields, slog.Int("status", apiErr.StatusCode))
		}

		slog.Warn("model call failed, failing over", fields...)
		provider.models.Fail(selected, err)
	}

	if lastErr == nil {
		return nil, fmt.Errorf("no models in rotation")
	}

	return nil, fmt.Errorf("every model failed: %w", lastErr)
}
