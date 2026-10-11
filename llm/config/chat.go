package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/Y2Kwastaken/model-citizen/llm/clients"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

type Chat struct {
	HistorySize        int                    `toml:"history_size"`
	ReplyTimeout       time.Duration          `toml:"reply_timeout"`
	PerClientTimeout   time.Duration          `toml:"per_client_timeout"`
	DefaultPersonality string                 `toml:"default_personality"`
	Personalities      map[string]Personality `toml:"personalities"`
	Rotation           Rotation               `toml:"rotation"`
	Clients            []ChatClient           `toml:"clients"`
}

type ChatClient struct {
	BaseURL   string `toml:"base_url"`
	APIKeyEnv string `toml:"api_key_env"`

	clients.ChatSettings
}

func (c ChatClient) Options() []option.RequestOption {
	opts := []option.RequestOption{option.WithAPIKey(os.Getenv(c.APIKeyEnv))}
	if c.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(c.BaseURL))
	}
	return opts
}

func (c ChatClient) Client() clients.ChatClient {
	return *clients.NewChatClient(openai.NewClient(c.Options()...), c.ChatSettings)
}

func (c Chat) ChatClients() []clients.ChatClient {
	return chatClients(c.Clients)
}

func chatClients(entries []ChatClient) []clients.ChatClient {
	out := make([]clients.ChatClient, len(entries))
	for i, entry := range entries {
		out[i] = entry.Client()
	}
	return out
}

func (c Chat) Prompts() map[string]string {
	out := make(map[string]string, len(c.Personalities))
	for name, personality := range c.Personalities {
		out[name] = personality.Prompt
	}
	return out
}

func (c Chat) validate() error {
	var errs []error

	errs = append(errs, validatePositive("chat.history_size", c.HistorySize))
	errs = append(errs, validatePositive("chat.reply_timeout", c.ReplyTimeout))
	errs = append(errs, validatePositive("chat.per_client_timeout", c.PerClientTimeout))

	if len(c.Personalities) == 0 {
		errs = append(errs, errors.New("chat.personalities needs at least one personality"))
	}
	if _, ok := c.Personalities[c.DefaultPersonality]; !ok {
		errs = append(errs, fmt.Errorf("chat.default_personality %q is not in chat.personalities", c.DefaultPersonality))
	}
	for name, personality := range c.Personalities {
		if _, err := os.Stat(personality.Prompt); err != nil {
			errs = append(errs, fmt.Errorf("chat.personalities.%s.prompt: %w", name, err))
		}
	}

	if err := c.Rotation.validate("chat"); err != nil {
		errs = append(errs, err)
	}

	if len(c.Clients) == 0 {
		errs = append(errs, errors.New("chat.clients needs at least one client"))
	}
	for i, client := range c.Clients {
		if err := client.validate(); err != nil {
			errs = append(errs, fmt.Errorf("chat.clients[%d] (%s): %w", i, client.Model, err))
		}
	}

	return errors.Join(errs...)
}

func (c ChatClient) validate() error {
	var errs []error

	errs = append(errs, validateNotEmpty("model", c.Model))
	errs = append(errs, validateAPIKeyEnv(c.APIKeyEnv))

	errs = append(errs, validateRange("temperature", c.Temperature, 0, 2))
	if c.TopP != nil && (*c.TopP <= 0 || *c.TopP > 1) {
		errs = append(errs, fmt.Errorf("top_p must be above 0 and at most 1, got %v", *c.TopP))
	}
	if c.MaxCompletionTokens != nil {
		errs = append(errs, validatePositive("max_completion_tokens", *c.MaxCompletionTokens))
	}
	errs = append(errs, validateRange("presence_penalty", c.PresencePenalty, -2, 2))
	errs = append(errs, validateRange("frequency_penalty", c.FrequencyPenalty, -2, 2))
	switch c.ReasoningEffort {
	case "", "minimal", "low", "medium", "high":
	default:
		errs = append(errs, fmt.Errorf("reasoning_effort must be minimal, low, medium or high, got %q", c.ReasoningEffort))
	}
	if len(c.Stop) > 4 {
		errs = append(errs, fmt.Errorf("stop allows at most 4 sequences, got %d", len(c.Stop)))
	}

	return errors.Join(errs...)
}
