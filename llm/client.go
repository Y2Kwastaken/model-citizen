package llm

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/disgoorg/snowflake/v2"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
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
		Model:           provider.model,
		Messages:        messages,
		ReasoningEffort: shared.ReasoningEffortNone,
	})
	if err != nil {
		return "", err
	}
	if len(completion.Choices) == 0 {
		return "", fmt.Errorf("model returned no choices")
	}

	return cleanReply(completion.Choices[0].Message.Content, orderedHistory), nil
}

// timestampLine matches a "[3:52 PM]"-style chat log line.
var timestampLine = regexp.MustCompile(`^\[\d{1,2}:\d{2}`)

// cleanReply cuts a reply off where the model stops answering and starts
// writing the rest of the conversation. Small models fed a "name: text"
// transcript will, some of the time, continue it: fake lines from the people in
// the room, or a whole timestamped log. Only names actually seen in the history
// are matched, so an ordinary "note: ..." is left alone.
func cleanReply(reply string, history []Message) string {
	names := make(map[string]struct{}, len(history))
	for _, message := range history {
		names[message.Name] = struct{}{}
	}

	knownSpeaker := func(line string) bool {
		name, _, ok := strings.Cut(line, ":")
		if !ok {
			return false
		}
		_, known := names[strings.TrimSpace(name)]
		return known
	}
	speakerLine := func(line string) bool {
		return timestampLine.MatchString(line) || knownSpeaker(line)
	}

	lines := strings.Split(strings.TrimSpace(reply), "\n")

	// the model sometimes opens by echoing a speaker tag before answering as
	// itself; keep what follows. a timestamped opener is a log, not an answer.
	if len(lines) > 0 && knownSpeaker(lines[0]) {
		_, rest, _ := strings.Cut(lines[0], ":")
		lines[0] = strings.TrimSpace(rest)
	}

	for i := 0; i < len(lines); i++ {
		if speakerLine(lines[i]) {
			lines = lines[:i]
			break
		}
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func (provider *BasicClientProvider) History() HistoryProvider {
	return provider.history
}
