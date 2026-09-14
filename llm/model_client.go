package llm

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

// personality file
//
//go:embed prompts/system.md
var defaultSystemPrompt string

// a perModelTimeout
const perModelTimeout = kill_threshold

// maxToolRounds is how many times in one reply the model may call tools before
// it has to answer in words. One is the normal case; two lets it react to a
// result.
const maxToolRounds = 2

type BrainClient interface {
	// Chat sends the channel's recorded history and returns the model's reply,
	// running any tools it asks for along the way.
	Chat(ctx context.Context, origin Origin) (string, error)
	// the history provision for this brain
	History() HistoryProvider
	Tools() ModelTools
}

type BasicClientProvider struct {
	models       ModelManager
	systemPrompt string
	history      HistoryProvider
	tools        ModelTools
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
		tools:        NewModelTools(),
	}

	return &provider, nil
}

func (provider *BasicClientProvider) Chat(ctx context.Context, origin Origin) (string, error) {
	history := provider.history
	channel := origin.Channel

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

	return provider.act(ctx, openai.ChatCompletionNewParams{
		Messages:        messages,
		Tools:           provider.tools.Build(),
		ReasoningEffort: shared.ReasoningEffortNone,
		Temperature:     openai.Float(temperature),
		TopP:            openai.Float(topP),
		MaxTokens:       openai.Int(maxReplyTokens),
	}, orderedHistory, origin)
}

// act runs the model until it answers in words. Each round it either calls
// tools, which run and report back into the conversation, or writes a reply.
// The reply then takes the usual judge pass with the tools withdrawn, so a
// redraw can never run an action twice.
func (provider *BasicClientProvider) act(ctx context.Context, params openai.ChatCompletionNewParams, history []Message, origin Origin) (string, error) {
	for round := 0; ; round++ {
		// the last round is words only
		if round >= maxToolRounds {
			params.Tools = nil
		}

		completion, err := provider.complete(ctx, params)
		if err != nil {
			return "", err
		}
		if len(completion.Choices) == 0 {
			return "", fmt.Errorf("model returned no choices")
		}

		message := completion.Choices[0].Message
		if len(message.ToolCalls) == 0 || params.Tools == nil {
			params.Tools = nil
			return draw(ctx, provider.complete, params, history, completion)
		}

		// the assistant turn that made the calls has to precede their results
		params.Messages = append(params.Messages, message.ToParam())
		for _, call := range message.ToolCalls {
			result := provider.invoke(ctx, call, origin)
			params.Messages = append(params.Messages, openai.ToolMessage(result, call.ID))
		}
	}
}

// invoke runs one tool call and returns what to tell the model happened.
func (provider *BasicClientProvider) invoke(ctx context.Context, call openai.ChatCompletionMessageToolCallUnion, origin Origin) string {
	name := call.Function.Name
	tool, ok := provider.tools.Lookup(name)
	if !ok {
		slog.Warn("model called a tool that does not exist", slog.String("tool", name))
		return "there is no tool called " + name
	}

	slog.Info("running tool",
		slog.String("tool", name),
		slog.String("arguments", call.Function.Arguments),
		slog.String("guild_id", origin.Guild.String()),
	)
	result := tool.Handle(ctx, Invocation{Origin: origin, Arguments: call.Function.Arguments})
	slog.Debug("tool result", slog.String("tool", name), slog.String("result", result))
	return result
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

func (provider *BasicClientProvider) Tools() ModelTools {
	return provider.tools
}
