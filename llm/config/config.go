package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/Y2Kwastaken/model-citizen/shared"
)

// Config is the llm side of the bot, read once at startup and never changed
type Config struct {
	Chat Chat `toml:"chat"`
	// optional, without it the llm doesn't transcribe
	Stt Stt `toml:"stt"`
	// optional, without it the llm doesn't speak
	Tts Tts `toml:"tts"`
}

// a system prompt and the voice it speaks with
type Personality struct {
	// system prompt file. Written relative to the config directory, Load resolves
	// it so it opens from any working directory
	Prompt string `toml:"prompt"`
	// a name in [tts.voices], empty never speaks
	Voice string `toml:"voice"`
}

// Rotation is how a call's latency is judged, see shared.RotationPolicy
type Rotation struct {
	Reward time.Duration `toml:"reward"`
	Punish time.Duration `toml:"punish"`
	Kill   time.Duration `toml:"kill"`
	Parole time.Duration `toml:"parole"`
}

// the files in the llm config directory. chat is required, leave stt or tts
// out and the llm doesn't transcribe or speak
const (
	chatFile = "chat.toml"
	sttFile  = "stt.toml"
	ttsFile  = "tts.toml"
)

// every key chat.toml must set, there are no defaults
var requiredChatKeys = [][]string{
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

// every key tts.toml must set when it's there
var requiredTtsKeys = [][]string{
	{"tts", "timeout"},
	{"tts", "per_client_timeout"},
	{"tts", "rotation", "reward"},
	{"tts", "rotation", "punish"},
	{"tts", "rotation", "kill"},
	{"tts", "rotation", "parole"},
	{"tts", "voices"},
}

// every key stt.toml must set when it's there
var requiredSttKeys = [][]string{
	{"stt", "language"},
	{"stt", "timeout"},
	{"stt", "per_client_timeout"},
	{"stt", "rotation", "reward"},
	{"stt", "rotation", "punish"},
	{"stt", "rotation", "kill"},
	{"stt", "rotation", "parole"},
}

// Load decodes the config files in dir. There are no defaults, so each file
// that's there must set every one of its required keys.
func Load(dir string) (Config, error) {
	var cfg Config

	if err := decodeFile(filepath.Join(dir, chatFile), &cfg, requiredChatKeys); err != nil {
		return Config{}, err
	}
	stt, err := decodeOptionalFile(filepath.Join(dir, sttFile), &cfg, requiredSttKeys)
	if err != nil {
		return Config{}, err
	}
	tts, err := decodeOptionalFile(filepath.Join(dir, ttsFile), &cfg, requiredTtsKeys)
	if err != nil {
		return Config{}, err
	}

	for name, personality := range cfg.Chat.Personalities {
		if !filepath.IsAbs(personality.Prompt) {
			personality.Prompt = filepath.Join(dir, personality.Prompt)
			cfg.Chat.Personalities[name] = personality
		}
	}

	if err := cfg.validate(stt, tts); err != nil {
		return Config{}, fmt.Errorf("invalid llm config %s: %w", dir, err)
	}

	return cfg, nil
}

// decodes one file into cfg, failing on unknown keys and missing required ones
func decodeFile(path string, cfg *Config, required [][]string) error {
	md, err := toml.DecodeFile(path, cfg)
	if err != nil {
		return fmt.Errorf("reading llm config %s: %w", path, err)
	}

	if keys := unknownKeys(md); len(keys) != 0 {
		return fmt.Errorf("unknown keys in llm config %s: %s", path, strings.Join(keys, ", "))
	}

	if keys := missingKeys(md, required); len(keys) != 0 {
		return fmt.Errorf("missing keys in llm config %s: %s", path, strings.Join(keys, ", "))
	}

	return nil
}

// decodeFile for a file that can be left out, reports whether it was there
func decodeOptionalFile(path string, cfg *Config, required [][]string) (bool, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return true, decodeFile(path, cfg, required)
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

func missingKeys(md toml.MetaData, required [][]string) []string {
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

func (c Config) validate(stt bool, tts bool) error {
	var errs []error

	if err := c.Chat.validate(); err != nil {
		errs = append(errs, err)
	}

	// a personality's voice has to be one tts.toml can speak
	for name, personality := range c.Chat.Personalities {
		if personality.Voice == "" {
			continue
		}
		if !tts {
			errs = append(errs, fmt.Errorf("chat.personalities.%s.voice needs a tts.toml", name))
		} else if _, ok := c.Tts.Voices[personality.Voice]; !ok {
			errs = append(errs, fmt.Errorf("chat.personalities.%s.voice %q is not in tts.voices", name, personality.Voice))
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
	errs = append(errs, validatePositive(section+".rotation.parole", r.Parole))
	return errors.Join(errs...)
}
