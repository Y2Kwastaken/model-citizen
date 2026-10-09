package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Logging Logging `toml:"logging"`
	Voice   Voice   `toml:"voice"`
	Music   Music   `toml:"music"`
	Listen  Listen  `toml:"listen"`
}

type Logging struct {
	// one of debug, info, warn, error
	Level string `toml:"level"`
}

// settings shared by everything that plays audio in a voice channel
type Voice struct {
	JoinTimeout time.Duration `toml:"join_timeout"`
	// per mixer layer jitter buffer, each frame is 20 ms
	LayerBufferFrames int `toml:"layer_buffer_frames"`
}

type Music struct {
	QueueCapacity int           `toml:"queue_capacity"`
	VolumeDB      float32       `toml:"volume_db"`
	SearchTimeout time.Duration `toml:"search_timeout"`
	Cache         Cache         `toml:"cache"`
}

// wake word listening in voice channels
type Listen struct {
	// path to libonnxruntime.so
	Library string `toml:"onnxruntime"`
	// holds the wake word models
	Directory string `toml:"directory"`
	// the wake word model file in directory
	Model string `toml:"model"`
	// the ways transcription spells the name in the wake word, taken out of
	// transcripts so the model never sees it
	WakeNames []string `toml:"wake_names"`
	// score from 0 to 1 that counts as hearing the wake word
	Threshold float32 `toml:"threshold"`
	// how soon the same person can wake it again
	Debounce time.Duration `toml:"debounce"`
	// audio kept per speaker
	Window time.Duration `toml:"window"`
	// conversation from before the wake word sent along with the command
	Context time.Duration `toml:"context"`
	// pause that ends a command
	CommandQuiet time.Duration `toml:"command_quiet"`
	// longest command listened to
	CommandMax time.Duration `toml:"command_max"`
	// shorter sounds are noise and aren't transcribed
	MinUtterance time.Duration `toml:"min_utterance"`
	// in dBFS, quieter audio counts as silence when waiting for a command to end
	QuietLevel float64 `toml:"quiet_level"`
}

type Cache struct {
	Directory string `toml:"directory"`
	MaxTracks int    `toml:"max_tracks"`
}

// Default mirrors the values the bot used before it was configurable
func Default() Config {
	return Config{
		Logging: Logging{
			Level: "info",
		},
		Voice: Voice{
			JoinTimeout:       5 * time.Second,
			LayerBufferFrames: 25,
		},
		Music: Music{
			QueueCapacity: 25,
			VolumeDB:      -6,
			SearchTimeout: 2 * time.Minute,
			Cache: Cache{
				Directory: "/songs/",
				MaxTracks: 50,
			},
		},
		Listen: Listen{
			Library:      "/usr/local/lib/libonnxruntime.so",
			Directory:    "/assets/wake/",
			Model:        "hey_model.onnx",
			WakeNames:    []string{"model"},
			Threshold:    0.12,
			Debounce:     3 * time.Second,
			Window:       30 * time.Second,
			Context:      10 * time.Second,
			CommandQuiet: 700 * time.Millisecond,
			CommandMax:   10 * time.Second,
			MinUtterance: 400 * time.Millisecond,
			QuietLevel:   -40,
		},
	}
}

// Load decodes the file at path over the defaults, so the file only needs the
// keys it changes. A missing file is not an error, the defaults are used.
func Load(path string) (Config, error) {
	cfg := Default()

	md, err := toml.DecodeFile(path, &cfg)
	if errors.Is(err, fs.ErrNotExist) {
		slog.Info("no config file found, using defaults", slog.String("path", path))
		return cfg, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("reading config %s: %w", path, err)
	}

	if undecoded := md.Undecoded(); len(undecoded) != 0 {
		keys := make([]string, len(undecoded))
		for i, key := range undecoded {
			keys[i] = key.String()
		}
		return Config{}, fmt.Errorf("unknown keys in config %s: %s", path, strings.Join(keys, ", "))
	}

	if err := cfg.validate(); err != nil {
		return Config{}, fmt.Errorf("invalid config %s: %w", path, err)
	}

	return cfg, nil
}

// SlogLevel converts the configured level, validate guarantees it parses
func (l Logging) SlogLevel() slog.Level {
	var level slog.Level
	level.UnmarshalText([]byte(l.Level))
	return level
}

func (c Config) validate() error {
	var errs []error

	var level slog.Level
	if err := level.UnmarshalText([]byte(c.Logging.Level)); err != nil {
		errs = append(errs, fmt.Errorf("logging.level must be debug, info, warn or error, got %q", c.Logging.Level))
	}

	if c.Voice.JoinTimeout <= 0 {
		errs = append(errs, fmt.Errorf("voice.join_timeout must be positive, got %s", c.Voice.JoinTimeout))
	}
	if c.Voice.LayerBufferFrames <= 0 {
		errs = append(errs, fmt.Errorf("voice.layer_buffer_frames must be positive, got %d", c.Voice.LayerBufferFrames))
	}

	if c.Music.QueueCapacity <= 0 {
		errs = append(errs, fmt.Errorf("music.queue_capacity must be positive, got %d", c.Music.QueueCapacity))
	}
	if c.Music.SearchTimeout <= 0 {
		errs = append(errs, fmt.Errorf("music.search_timeout must be positive, got %s", c.Music.SearchTimeout))
	}
	if c.Music.Cache.Directory == "" {
		errs = append(errs, errors.New("music.cache.directory must not be empty"))
	}
	if c.Music.Cache.MaxTracks <= 0 {
		errs = append(errs, fmt.Errorf("music.cache.max_tracks must be positive, got %d", c.Music.Cache.MaxTracks))
	}

	if c.Listen.Library == "" {
		errs = append(errs, errors.New("listen.onnxruntime must not be empty"))
	}
	if c.Listen.Directory == "" {
		errs = append(errs, errors.New("listen.directory must not be empty"))
	}
	if c.Listen.Model == "" {
		errs = append(errs, errors.New("listen.model must not be empty"))
	}
	if len(c.Listen.WakeNames) == 0 {
		errs = append(errs, errors.New("listen.wake_names must not be empty"))
	}
	if slices.Contains(c.Listen.WakeNames, "") {
		errs = append(errs, errors.New("listen.wake_names must not contain an empty name"))
	}
	if c.Listen.Threshold <= 0 || c.Listen.Threshold > 1 {
		errs = append(errs, fmt.Errorf("listen.threshold must be above 0 and at most 1, got %v", c.Listen.Threshold))
	}
	if c.Listen.Debounce < 0 {
		errs = append(errs, fmt.Errorf("listen.debounce must not be negative, got %s", c.Listen.Debounce))
	}
	if c.Listen.Context < 0 {
		errs = append(errs, fmt.Errorf("listen.context must not be negative, got %s", c.Listen.Context))
	}
	if c.Listen.CommandQuiet <= 0 {
		errs = append(errs, fmt.Errorf("listen.command_quiet must be positive, got %s", c.Listen.CommandQuiet))
	}
	if c.Listen.CommandMax <= 0 {
		errs = append(errs, fmt.Errorf("listen.command_max must be positive, got %s", c.Listen.CommandMax))
	}
	if c.Listen.QuietLevel >= 0 || c.Listen.QuietLevel < -100 {
		errs = append(errs, fmt.Errorf("listen.quiet_level must be between -100 and 0 dBFS, got %v", c.Listen.QuietLevel))
	}
	if c.Listen.MinUtterance < 0 {
		errs = append(errs, fmt.Errorf("listen.min_utterance must not be negative, got %s", c.Listen.MinUtterance))
	}
	// the context and the whole command have to still be in the window when they're collected
	if needed := c.Listen.Context + c.Listen.CommandMax + c.Listen.CommandQuiet; c.Listen.Window < needed {
		errs = append(errs, fmt.Errorf("listen.window must be at least context + command_max + command_quiet (%s), got %s", needed, c.Listen.Window))
	}

	return errors.Join(errs...)
}
