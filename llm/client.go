package llm

import (
	"context"
	_ "embed"
	"fmt"
	"os"

	"github.com/disgoorg/snowflake/v2"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// The bot's personality. Embedded rather than read from disk so it ships in the
// binary — data/ is excluded by .dockerignore and there is no path to resolve.
//
//go:embed prompts/system.md
var defaultSystemPrompt string

type BrainClient interface {
	// Chat sends the channel's recorded history and returns the model's reply.
	//
	// The API itself is stateless; the brain owns the history via History().
	Chat(ctx context.Context, channel snowflake.ID) (string, error)
	// the history provision for this brain
	History() HistoryProvider
}

type BasicClientProvider struct {
	client       openai.Client
	model        string
	systemPrompt string
	history      HistoryProvider
}

// NewBrainClient builds a client from environment variable *names*, matching
// how Start takes its Discord token, so deployment config stays in data/.env.
func NewBrainClient(modelAuthKey string, modelNameKey string) (BrainClient, error) {
	key := os.Getenv(modelAuthKey)
	if key == "" {
		return nil, fmt.Errorf("key not set in environment at variable %s", modelAuthKey)
	}

	modelName := os.Getenv(modelNameKey)
	if modelName == "" {
		return nil, fmt.Errorf("model name not set in environment at variable %s", modelNameKey)
	}

	client := openai.NewClient(
		option.WithBaseURL("https://integrate.api.nvidia.com/v1"),
		option.WithAPIKey(key),
	)

	provider := BasicClientProvider{
		client:       client,
		model:        modelName,
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

	// The system prompt leads every request and never varies, which is also
	// what providers key prompt caching on.
	orderedHistory := history.OrderedHistory(channel)
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(orderedHistory)+1)
	messages = append(messages, openai.SystemMessage(provider.systemPrompt))

	for _, message := range orderedHistory {
		switch message.Who {
		case Self:
			messages = append(messages, openai.AssistantMessage(message.Content))
		default:
			messages = append(messages, openai.UserMessage(message.Name+": "+message.Content))
		}
	}

	completion, err := provider.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:    provider.model,
		Messages: messages,
	})
	if err != nil {
		return "", err
	}
	if len(completion.Choices) == 0 {
		return "", fmt.Errorf("model returned no choices")
	}

	return completion.Choices[0].Message.Content, nil
}

func (provider *BasicClientProvider) History() HistoryProvider {
	return provider.history
}
