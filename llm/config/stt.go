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

// speech to text, every key is required once stt.toml is there
type Stt struct {
	// passed to every service, empty lets them guess
	Language string `toml:"language"`
	// how long one clip may take across every failover attempt
	Timeout time.Duration `toml:"timeout"`
	// how long one attempt on one client may take before failing over
	PerClientTimeout time.Duration `toml:"per_client_timeout"`
	Rotation         Rotation      `toml:"rotation"`
	// tried in order, a failing one is benched and the next is tried
	Clients []SttClient `toml:"clients"`
}

// SttClient is one entry in the speech to text rotation
type SttClient struct {
	// the shape of the service's api: openai (default), deepgram or assemblyai
	API   string `toml:"api"`
	Model string `toml:"model"`
	// required for deepgram and assemblyai, empty uses OpenAI's for openai
	BaseURL string `toml:"base_url"`
	// name of the environment variable holding the API key
	APIKeyEnv string `toml:"api_key_env"`
}

// whether the llm transcribes, validate guarantees a client once stt.toml is there
func (s Stt) Enabled() bool {
	return len(s.Clients) > 0
}

// builds the transcriber this entry describes
func (c SttClient) Transcriber() clients.Transcriber {
	key := os.Getenv(c.APIKeyEnv)
	switch c.API {
	case "deepgram":
		return clients.NewDeepgramTranscriber(c.BaseURL, key, c.Model)
	case "assemblyai":
		return clients.NewAssemblyAITranscriber(c.BaseURL, key, c.Model)
	default:
		opts := []option.RequestOption{option.WithAPIKey(key)}
		if c.BaseURL != "" {
			opts = append(opts, option.WithBaseURL(c.BaseURL))
		}
		return clients.NewOpenAITranscriber(openai.NewClient(opts...), c.Model)
	}
}

// builds every configured transcriber, in rotation order
func (s Stt) Transcribers() []clients.Transcriber {
	out := make([]clients.Transcriber, len(s.Clients))
	for i, client := range s.Clients {
		out[i] = client.Transcriber()
	}
	return out
}

func (s Stt) validate() error {
	var errs []error

	errs = append(errs, validatePositive("stt.timeout", s.Timeout))
	errs = append(errs, validatePositive("stt.per_client_timeout", s.PerClientTimeout))
	if err := s.Rotation.validate("stt"); err != nil {
		errs = append(errs, err)
	}

	if len(s.Clients) == 0 {
		errs = append(errs, errors.New("stt.clients needs at least one client"))
	}
	for i, client := range s.Clients {
		if err := client.validate(); err != nil {
			errs = append(errs, fmt.Errorf("stt.clients[%d] (%s): %w", i, client.Model, err))
		}
	}

	return errors.Join(errs...)
}

func (c SttClient) validate() error {
	var errs []error

	switch c.API {
	case "", "openai":
	case "deepgram", "assemblyai":
		if c.BaseURL == "" {
			errs = append(errs, fmt.Errorf("base_url is required for %s", c.API))
		}
	default:
		errs = append(errs, fmt.Errorf("api must be openai, deepgram or assemblyai, got %q", c.API))
	}
	errs = append(errs, validateNotEmpty("model", c.Model))
	errs = append(errs, validateAPIKeyEnv(c.APIKeyEnv))

	return errors.Join(errs...)
}
