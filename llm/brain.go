package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Y2Kwastaken/model-citizen/llm/model"
	"github.com/disgoorg/snowflake/v2"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

const (
	perModelTimeout = 10 * time.Second
	// tool calls in one reply
	maxToolRounds = 2
)

// Config is where each rotation comes from.
type Config struct {
	// the personality prompt and messages kept per channel, from config/model.json
	SystemPrompts  map[string]string
	SelectedPrompt string
	HistorySize    int
	Temperature    float64
	TopP           float64

	TextModelsFile   string
	VoiceModelsFile  string
	SpeechModelsFile string
}

type BrainProvider struct {
	systemPrompts map[string]string
	defaultPrompt string
	// personality picked per guild, defaultPrompt when unset
	promptLock  sync.RWMutex
	guildPrompt map[snowflake.ID]string
	temperature float64
	topP        float64
	// a feature is supported iff it has a rotation
	rotations map[model.ModelFeature]model.ModelManager
	history   model.HistoryProvider
	tools     model.ModelTools
	memory    model.MemorySet
}

// NewBrainLanguageModel builds a rotation per feature. Chat is required; a
// voice or speech rotation that cannot be built drops its feature.
func NewBrainLanguageModel(config Config) (model.LanguageModel, error) {
	rotations := make(map[model.ModelFeature]model.ModelManager)

	text, err := model.NewModelManager(config.TextModelsFile, model.Chat)
	if err != nil {
		return nil, fmt.Errorf("chat models %s: %w", config.TextModelsFile, err)
	}
	rotations[model.Chat] = text

	if voice, err := model.NewModelManager(config.VoiceModelsFile, model.STT); err != nil {
		slog.Warn("transcription disabled", slog.String("file", config.VoiceModelsFile), slog.Any("error", err))
	} else {
		rotations[model.STT] = voice
	}

	if speech, err := model.NewModelManager(config.SpeechModelsFile, model.TTS); err != nil {
		slog.Warn("speech disabled", slog.String("file", config.SpeechModelsFile), slog.Any("error", err))
	} else {
		rotations[model.TTS] = speech
	}

	return &BrainProvider{
		systemPrompts: config.SystemPrompts,
		defaultPrompt: config.SelectedPrompt,
		guildPrompt:   make(map[snowflake.ID]string),
		temperature:   config.Temperature,
		topP:          config.TopP,
		rotations:     rotations,
		history:       model.NewChatHistory(config.HistorySize),
		tools:         model.NewModelTools(),
		memory:        model.NewMemorySet(map[model.MemoryType]model.MemoryProvider{model.SHORT_TERM: model.NewShortTerm(-1)}),
	}, nil
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

func (provider *BrainProvider) MemorySet() model.MemorySet {
	return provider.memory
}

// implement tweaks

func (provider *BrainProvider) SetPersonality(guild snowflake.ID, name string) error {
	if _, ok := provider.systemPrompts[name]; !ok {
		return fmt.Errorf("no known system prompt %s", name)
	}

	provider.promptLock.Lock()
	provider.guildPrompt[guild] = name
	provider.promptLock.Unlock()
	return nil
}

// systemPrompt is the prompt of the personality guild picked, or the default.
func (provider *BrainProvider) systemPrompt(guild snowflake.ID) string {
	provider.promptLock.RLock()
	name, ok := provider.guildPrompt[guild]
	provider.promptLock.RUnlock()
	if !ok {
		name = provider.defaultPrompt
	}
	return provider.systemPrompts[name]
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
	memoryHistory := provider.memory.AllOrderedMemories()
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(memoryHistory)+len(orderedHistory)+1)
	messages = append(messages, openai.SystemMessage(provider.systemPrompt(origin.Guild)))
	for _, memory := range memoryHistory {
		messages = append(messages, openai.SystemMessage("["+memory.At.Format("2006-01-02 15:04:05")+"]Memory ["+memory.Name+"]: "+memory.Memory))
	}

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
		Temperature:     openai.Float(provider.temperature),
		TopP:            openai.Float(provider.topP),
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

// Speak reads text out. Which voice answers is the rotation's business, but a
// voice is the bot's whole character to whoever is listening, so a roster with
// more than one entry should read as an outage plan rather than a choice.
func (provider *BrainProvider) Speak(ctx context.Context, text string) (model.Clip, error) {
	if !provider.HasFeature(model.TTS) {
		return model.Clip{}, fmt.Errorf("this model does not speak")
	}

	return attempt(ctx, provider.rotations[model.TTS], func(ctx context.Context, selected model.Model) (model.Clip, error) {
		return selected.Speak(ctx, text)
	})
}

func (provider *BrainProvider) doChat(ctx context.Context, params openai.ChatCompletionNewParams, history []model.Message, origin model.Origin) (string, error) {
	for round := 0; ; round++ {
		if round >= maxToolRounds {
			params.Tools = nil
		}
		params.MaxTokens = openai.Int(maxReplyTokens)
		if params.Tools != nil {
			params.MaxTokens = openai.Int(maxReplyTokens + toolRoundBonus)
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
			params.MaxTokens = openai.Int(maxReplyTokens)
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
		// ReasoningEffortNone is ignored by the NIM models. enable_thinking is
		// the vllm switch nemotron honours; muse-glimmer cannot stop thinking
		// and only takes reasoning_strength. ollama drops unknown fields.
		return selected.Client.Chat.Completions.New(ctx, params,
			option.WithJSONSet("chat_template_kwargs", map[string]any{
				"enable_thinking":    false,
				"thinking":           false,
				"reasoning_strength": "low",
			}))
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
