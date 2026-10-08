package clients

import (
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

type ChatSettings struct {
	Model string `toml:"model"`
	// sampling, 0-2. Reasoning models reject this, leave it nil for them
	Temperature *float64 `toml:"temperature"`
	// nucleus sampling, 0-1. Usually set this or Temperature, not both
	TopP *float64 `toml:"top_p"`
	// caps the reply length, reasoning tokens count toward it
	MaxCompletionTokens *int64 `toml:"max_completion_tokens"`
	// -2 to 2, positive values discourage repeating topics
	PresencePenalty *float64 `toml:"presence_penalty"`
	// -2 to 2, positive values discourage repeating the same words
	FrequencyPenalty *float64 `toml:"frequency_penalty"`
	// best effort deterministic sampling
	Seed *int64 `toml:"seed"`
	// "minimal" | "low" | "medium" | "high", reasoning models only. Empty omits it
	ReasoningEffort string `toml:"reasoning_effort"`
	// up to 4 sequences that end the reply early
	Stop []string `toml:"stop"`
	// extra top level JSON fields for provider specific vendor options.
	ExtraBody map[string]any `toml:"extra_body"`
}

// ChatClient wraps an openai client with the settings used to build its requests.
// Settings are fixed at construction and never change for the client's lifetime.
type ChatClient struct {
	Client   openai.Client
	settings ChatSettings
}

func NewChatClient(client openai.Client, settings ChatSettings) *ChatClient {
	return &ChatClient{Client: client, settings: settings}
}

func (c *ChatClient) Settings() ChatSettings {
	return c.settings
}

// builds completion params from the client's settings and the given messages
func (c *ChatClient) Params(messages []openai.ChatCompletionMessageParamUnion) openai.ChatCompletionNewParams {
	s := c.settings

	params := openai.ChatCompletionNewParams{
		Model:    s.Model,
		Messages: messages,
	}

	if s.Temperature != nil {
		params.Temperature = openai.Float(*s.Temperature)
	}
	if s.TopP != nil {
		params.TopP = openai.Float(*s.TopP)
	}
	if s.MaxCompletionTokens != nil {
		params.MaxCompletionTokens = openai.Int(*s.MaxCompletionTokens)
	}
	if s.PresencePenalty != nil {
		params.PresencePenalty = openai.Float(*s.PresencePenalty)
	}
	if s.FrequencyPenalty != nil {
		params.FrequencyPenalty = openai.Float(*s.FrequencyPenalty)
	}
	if s.Seed != nil {
		params.Seed = openai.Int(*s.Seed)
	}
	if s.ReasoningEffort != "" {
		params.ReasoningEffort = shared.ReasoningEffort(s.ReasoningEffort)
	}
	if len(s.Stop) > 0 {
		params.Stop = openai.ChatCompletionNewParamsStopUnion{OfStringArray: s.Stop}
	}
	if len(s.ExtraBody) > 0 {
		params.SetExtraFields(s.ExtraBody)
	}

	return params
}
