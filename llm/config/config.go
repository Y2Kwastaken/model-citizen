package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/Y2Kwastaken/model-citizen/llm/clients"
	"github.com/Y2Kwastaken/model-citizen/shared"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// Config is the llm side of the bot, read once at startup and never changed
type Config struct {
	Chat Chat `toml:"chat"`
	// optional, without it the llm doesn't transcribe
	Stt Stt `toml:"stt"`
	// optional, without it the llm doesn't speak
	Tts Tts `toml:"tts"`
}

type Chat struct {
	// messages kept per channel and sent with each reply
	HistorySize int `toml:"history_size"`
	// how long one reply may take across every failover attempt
	ReplyTimeout time.Duration `toml:"reply_timeout"`
	// how long one attempt on one client may take before failing over
	PerClientTimeout time.Duration `toml:"per_client_timeout"`
	// personality every conversation starts with, a key of Personalities
	DefaultPersonality string `toml:"default_personality"`
	// personality name to its prompt and voice
	Personalities map[string]Personality `toml:"personalities"`
	Rotation      Rotation               `toml:"rotation"`
	// tried in order, the rotation moves off ones that are slow or down
	Clients []ChatClient `toml:"clients"`
}

// a system prompt and the voice it speaks with
type Personality struct {
	// system prompt file. Written relative to the config file, Load resolves
	// it so it opens from any working directory
	Prompt string `toml:"prompt"`
	// a name in [tts.voices], empty never speaks
	Voice string `toml:"voice"`
}

// text to speech, every key is required once [tts] is in the file
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

// speech to text, every key is required once [stt] is in the file
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

// Rotation is how a call's latency is judged, see shared.RotationPolicy
type Rotation struct {
	Reward time.Duration `toml:"reward"`
	Punish time.Duration `toml:"punish"`
	Kill   time.Duration `toml:"kill"`
	Parole time.Duration `toml:"parole"`
}

// ChatClient is one entry in the chat rotation: where to connect and what to send
type ChatClient struct {
	// OpenAI compatible endpoint, empty uses OpenAI's
	BaseURL string `toml:"base_url"`
	// name of the environment variable holding the API key, the key itself
	// stays in .env
	APIKeyEnv string `toml:"api_key_env"`

	// model, sampling and extra body, inlined so they sit beside base_url
	clients.ChatSettings
}

// every key the file must set, there are no defaults
var requiredKeys = [][]string{
	{"chat", "history_size"},
	{"chat", "reply_timeout"},
	{"chat", "per_client_timeout"},
	{"chat", "default_personality"},
	{"chat", "personalities"},
	{"chat", "rotation", "reward"},
	{"chat", "rotation", "punish"},
	{"chat", "rotation", "kill"},
	{"chat", "rotation", "parole"},
}

// every key [tts] must set when it's in the file
var requiredTtsKeys = [][]string{
	{"tts", "timeout"},
	{"tts", "per_client_timeout"},
	{"tts", "rotation", "reward"},
	{"tts", "rotation", "punish"},
	{"tts", "rotation", "kill"},
	{"tts", "rotation", "parole"},
	{"tts", "voices"},
}

// every key [stt] must set when it's in the file
var requiredSttKeys = [][]string{
	{"stt", "language"},
	{"stt", "timeout"},
	{"stt", "per_client_timeout"},
	{"stt", "rotation", "reward"},
	{"stt", "rotation", "punish"},
	{"stt", "rotation", "kill"},
	{"stt", "rotation", "parole"},
}

// Load decodes the file at path. There are no defaults, so the file must set
// every key in requiredKeys and at least one client.
func Load(path string) (Config, error) {
	var cfg Config

	md, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("reading llm config %s: %w", path, err)
	}

	if keys := unknownKeys(md); len(keys) != 0 {
		return Config{}, fmt.Errorf("unknown keys in llm config %s: %s", path, strings.Join(keys, ", "))
	}

	if keys := missingKeys(md); len(keys) != 0 {
		return Config{}, fmt.Errorf("missing keys in llm config %s: %s", path, strings.Join(keys, ", "))
	}

	dir := filepath.Dir(path)
	for name, personality := range cfg.Chat.Personalities {
		if !filepath.IsAbs(personality.Prompt) {
			personality.Prompt = filepath.Join(dir, personality.Prompt)
			cfg.Chat.Personalities[name] = personality
		}
	}

	if err := cfg.validate(md.IsDefined("stt"), md.IsDefined("tts")); err != nil {
		return Config{}, fmt.Errorf("invalid llm config %s: %w", path, err)
	}

	return cfg, nil
}

// keys the file set that no field took. extra_body is a free form map, but
// toml still lists the keys nested in it as undecoded, so they are skipped
func unknownKeys(md toml.MetaData) []string {
	var keys []string
	for _, key := range md.Undecoded() {
		if len(key) > 3 && key[0] == "chat" && key[1] == "clients" && key[2] == "extra_body" {
			continue
		}
		keys = append(keys, key.String())
	}
	return keys
}

func missingKeys(md toml.MetaData) []string {
	required := requiredKeys
	if md.IsDefined("stt") {
		required = slices.Concat(required, requiredSttKeys)
	}
	if md.IsDefined("tts") {
		required = slices.Concat(required, requiredTtsKeys)
	}

	var keys []string
	for _, key := range required {
		if !md.IsDefined(key...) {
			keys = append(keys, strings.Join(key, "."))
		}
	}
	return keys
}

// Policy converts the rotation thresholds for shared.NewRotation
func (r Rotation) Policy() shared.RotationPolicy {
	return shared.RotationPolicy{Reward: r.Reward, Punish: r.Punish, Kill: r.Kill, Parole: r.Parole}
}

func (c ChatClient) Options() []option.RequestOption {
	opts := []option.RequestOption{option.WithAPIKey(os.Getenv(c.APIKeyEnv))}
	if c.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(c.BaseURL))
	}
	return opts
}

// Client builds the chat client this entry describes
func (c ChatClient) Client() clients.ChatClient {
	return *clients.NewChatClient(openai.NewClient(c.Options()...), c.ChatSettings)
}

// ChatClients builds every configured client, in rotation order
func (c Chat) ChatClients() []clients.ChatClient {
	out := make([]clients.ChatClient, len(c.Clients))
	for i, client := range c.Clients {
		out[i] = client.Client()
	}
	return out
}

// personality name to its prompt file, what NewBrainModel takes
func (c Chat) Prompts() map[string]string {
	out := make(map[string]string, len(c.Personalities))
	for name, personality := range c.Personalities {
		out[name] = personality.Prompt
	}
	return out
}

// whether the llm speaks, validate guarantees a voice once [tts] is set
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

// whether the llm transcribes, validate guarantees a client once [stt] is set
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

func (c Config) validate(stt bool, tts bool) error {
	var errs []error

	chat := c.Chat
	if chat.HistorySize <= 0 {
		errs = append(errs, fmt.Errorf("chat.history_size must be positive, got %d", chat.HistorySize))
	}
	if chat.ReplyTimeout <= 0 {
		errs = append(errs, fmt.Errorf("chat.reply_timeout must be positive, got %s", chat.ReplyTimeout))
	}
	if chat.PerClientTimeout <= 0 {
		errs = append(errs, fmt.Errorf("chat.per_client_timeout must be positive, got %s", chat.PerClientTimeout))
	}

	if len(chat.Personalities) == 0 {
		errs = append(errs, errors.New("chat.personalities needs at least one personality"))
	}
	if _, ok := chat.Personalities[chat.DefaultPersonality]; !ok {
		errs = append(errs, fmt.Errorf("chat.default_personality %q is not in chat.personalities", chat.DefaultPersonality))
	}
	for name, personality := range chat.Personalities {
		if _, err := os.Stat(personality.Prompt); err != nil {
			errs = append(errs, fmt.Errorf("chat.personalities.%s.prompt: %w", name, err))
		}
		if personality.Voice == "" {
			continue
		}
		if !tts {
			errs = append(errs, fmt.Errorf("chat.personalities.%s.voice needs a [tts] section", name))
		} else if _, ok := c.Tts.Voices[personality.Voice]; !ok {
			errs = append(errs, fmt.Errorf("chat.personalities.%s.voice %q is not in tts.voices", name, personality.Voice))
		}
	}

	if err := chat.Rotation.validate("chat"); err != nil {
		errs = append(errs, err)
	}

	if len(chat.Clients) == 0 {
		errs = append(errs, errors.New("chat.clients needs at least one client"))
	}
	for i, client := range chat.Clients {
		if err := client.validate(); err != nil {
			errs = append(errs, fmt.Errorf("chat.clients[%d] (%s): %w", i, client.Model, err))
		}
	}

	if stt {
		if err := c.Stt.validate(); err != nil {
			errs = append(errs, err)
		}
	}
	if tts {
		if err := c.Tts.validate(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (r Rotation) validate(section string) error {
	var errs []error
	if r.Reward <= 0 || r.Reward > r.Punish || r.Punish > r.Kill {
		errs = append(errs, fmt.Errorf("%s.rotation needs 0 < reward <= punish <= kill, got %s, %s, %s",
			section, r.Reward, r.Punish, r.Kill))
	}
	if r.Parole <= 0 {
		errs = append(errs, fmt.Errorf("%s.rotation.parole must be positive, got %s", section, r.Parole))
	}
	return errors.Join(errs...)
}

func (s Stt) validate() error {
	var errs []error

	if s.Timeout <= 0 {
		errs = append(errs, fmt.Errorf("stt.timeout must be positive, got %s", s.Timeout))
	}
	if s.PerClientTimeout <= 0 {
		errs = append(errs, fmt.Errorf("stt.per_client_timeout must be positive, got %s", s.PerClientTimeout))
	}
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

func (t Tts) validate() error {
	var errs []error

	if t.Timeout <= 0 {
		errs = append(errs, fmt.Errorf("tts.timeout must be positive, got %s", t.Timeout))
	}
	if t.PerClientTimeout <= 0 {
		errs = append(errs, fmt.Errorf("tts.per_client_timeout must be positive, got %s", t.PerClientTimeout))
	}
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
	if c.Voice == "" {
		errs = append(errs, errors.New("voice must not be empty"))
	}
	if c.APIKeyEnv == "" {
		errs = append(errs, errors.New("api_key_env must not be empty"))
	} else if os.Getenv(c.APIKeyEnv) == "" {
		errs = append(errs, fmt.Errorf("environment variable %s is not set", c.APIKeyEnv))
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
	if c.Model == "" {
		errs = append(errs, errors.New("model must not be empty"))
	}
	if c.APIKeyEnv == "" {
		errs = append(errs, errors.New("api_key_env must not be empty"))
	} else if os.Getenv(c.APIKeyEnv) == "" {
		errs = append(errs, fmt.Errorf("environment variable %s is not set", c.APIKeyEnv))
	}

	return errors.Join(errs...)
}

func (c ChatClient) validate() error {
	var errs []error

	if c.Model == "" {
		errs = append(errs, errors.New("model must not be empty"))
	}
	if c.APIKeyEnv == "" {
		errs = append(errs, errors.New("api_key_env must not be empty"))
	} else if os.Getenv(c.APIKeyEnv) == "" {
		errs = append(errs, fmt.Errorf("environment variable %s is not set", c.APIKeyEnv))
	}

	if c.Temperature != nil && (*c.Temperature < 0 || *c.Temperature > 2) {
		errs = append(errs, fmt.Errorf("temperature must be between 0 and 2, got %v", *c.Temperature))
	}
	if c.TopP != nil && (*c.TopP <= 0 || *c.TopP > 1) {
		errs = append(errs, fmt.Errorf("top_p must be above 0 and at most 1, got %v", *c.TopP))
	}
	if c.MaxCompletionTokens != nil && *c.MaxCompletionTokens <= 0 {
		errs = append(errs, fmt.Errorf("max_completion_tokens must be positive, got %d", *c.MaxCompletionTokens))
	}
	if c.PresencePenalty != nil && (*c.PresencePenalty < -2 || *c.PresencePenalty > 2) {
		errs = append(errs, fmt.Errorf("presence_penalty must be between -2 and 2, got %v", *c.PresencePenalty))
	}
	if c.FrequencyPenalty != nil && (*c.FrequencyPenalty < -2 || *c.FrequencyPenalty > 2) {
		errs = append(errs, fmt.Errorf("frequency_penalty must be between -2 and 2, got %v", *c.FrequencyPenalty))
	}
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
