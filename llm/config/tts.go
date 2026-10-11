package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/Y2Kwastaken/model-citizen/llm/clients"
)

// text to speech, every key is required once tts.toml is there
type Tts struct {
	// how long one reply may take to speak across every failover attempt
	Timeout time.Duration `toml:"timeout"`
	// how long one attempt on one client may take before failing over
	PerClientTimeout time.Duration `toml:"per_client_timeout"`
	Rotation         Rotation      `toml:"rotation"`
	// voice name to the clients that can speak it, tried in order. more than
	// one is an outage plan, a voice changing mid conversation is jarring
	Voices map[string][]TtsClient `toml:"voices"`
}

// TtsClient is one way of speaking a voice
type TtsClient struct {
	// the shape of the service's api: openai (default), mistral or google
	API string `toml:"api"`
	// required for openai and mistral, google's voice names the model too
	Model string `toml:"model"`
	Voice string `toml:"voice"`
	// required for mistral and google, empty uses OpenAI's for openai
	BaseURL string `toml:"base_url"`
	// name of the environment variable holding the API key
	APIKeyEnv string `toml:"api_key_env"`
}

// whether the llm speaks, validate guarantees a voice once tts.toml is there
func (t Tts) Enabled() bool {
	return len(t.Voices) > 0
}

// builds the speaker this entry describes
func (c TtsClient) Speaker() clients.Speaker {
	key := os.Getenv(c.APIKeyEnv)
	switch c.API {
	case "mistral":
		return clients.NewMistralSpeaker(c.BaseURL, key, c.Model, c.Voice)
	case "google":
		return clients.NewGoogleSpeaker(c.BaseURL, key, c.Voice)
	default:
		baseURL := c.BaseURL
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		return clients.NewOpenAISpeaker(baseURL, key, c.Model, c.Voice)
	}
}

// voice name to its speakers, in failover order
func (t Tts) Speakers() map[string][]clients.Speaker {
	out := make(map[string][]clients.Speaker, len(t.Voices))
	for name, voice := range t.Voices {
		for _, client := range voice {
			out[name] = append(out[name], client.Speaker())
		}
	}
	return out
}

func (t Tts) validate() error {
	var errs []error

	errs = append(errs, validatePositive("tts.timeout", t.Timeout))
	errs = append(errs, validatePositive("tts.per_client_timeout", t.PerClientTimeout))
	if err := t.Rotation.validate("tts"); err != nil {
		errs = append(errs, err)
	}

	if len(t.Voices) == 0 {
		errs = append(errs, errors.New("tts.voices needs at least one voice"))
	}
	for name, voice := range t.Voices {
		if len(voice) == 0 {
			errs = append(errs, fmt.Errorf("tts.voices.%s needs at least one client", name))
		}
		for i, client := range voice {
			if err := client.validate(); err != nil {
				errs = append(errs, fmt.Errorf("tts.voices.%s[%d] (%s): %w", name, i, client.Voice, err))
			}
		}
	}

	return errors.Join(errs...)
}

func (c TtsClient) validate() error {
	var errs []error

	switch c.API {
	case "", "openai":
		if c.Model == "" {
			errs = append(errs, errors.New("model is required for openai"))
		}
	case "mistral":
		if c.Model == "" {
			errs = append(errs, errors.New("model is required for mistral"))
		}
		if c.BaseURL == "" {
			errs = append(errs, errors.New("base_url is required for mistral"))
		}
	case "google":
		if c.BaseURL == "" {
			errs = append(errs, errors.New("base_url is required for google"))
		}
		if _, ok := clients.GoogleLanguage(c.Voice); !ok {
			errs = append(errs, fmt.Errorf("voice %q is not a google voice name, e.g. en-GB-Chirp3-HD-Charon", c.Voice))
		}
	default:
		errs = append(errs, fmt.Errorf("api must be openai, mistral or google, got %q", c.API))
	}
	errs = append(errs, validateNotEmpty("voice", c.Voice))
	errs = append(errs, validateAPIKeyEnv(c.APIKeyEnv))

	return errors.Join(errs...)
}
