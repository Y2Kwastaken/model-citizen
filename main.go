package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	discordbot "github.com/Y2Kwastaken/model-citizen/discord-bot"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/audio"
	"github.com/Y2Kwastaken/model-citizen/llm"
)

const (
	MODEL_FILE_KEY       = "MODEL_FILE"
	VOICE_MODEL_FILE_KEY = "VOICE_MODEL_FILE"
	MODEL_AUTH_KEY       = "MODEL_AUTH_KEY"
	MODEL_NAME_KEY       = "MODEL_NAME"
	MODEL_LINK_KEY       = "MODEL_LINK"
	DISCORD_KEY          = "DISCORD_KEY"
	ONNXRUNTIME_LIB_KEY  = "ONNXRUNTIME_LIB"
	WAKE_DIR_KEY         = "WAKE_DIR"
	WAKE_THRESHOLD_KEY   = "WAKE_THRESHOLD"

	// where the rotations are read from when the *_FILE variables are unset;
	// the compose file mounts data/*-models.json here
	defaultModelFile      = "text-models.json"
	defaultVoiceModelFile = "voice-models.json"

	// the wake word models, and the onnxruntime the Dockerfile installs
	defaultOnnxruntimeLib = "/usr/local/lib/libonnxruntime.so"
	defaultWakeDir        = "wakeword"
	wakeModel             = "hey_model.onnx"
	// hey_model.onnx scored 93% of clean utterances over this with one false
	// trigger per ~2 hours per speaker; live audio through Discord's voice
	// gate lands lower than clean, and misses cost more than the odd extra
	// wake, so it errs low. 0.3 halves the false triggers if they grate.
	defaultWakeThreshold = 0.2
)

func main() {
	ctx := context.Background()

	brain, err := llm.NewBrainLanguageModel(llm.Config{
		TextModelsFile:  envOr(MODEL_FILE_KEY, defaultModelFile),
		VoiceModelsFile: envOr(VOICE_MODEL_FILE_KEY, defaultVoiceModelFile),
		FallbackAuthKey: MODEL_AUTH_KEY,
		FallbackNameKey: MODEL_NAME_KEY,
		FallbackLinkKey: MODEL_LINK_KEY,
	})
	if err != nil {
		slog.Error("error while starting llm connector", slog.Any("err", err))
		os.Exit(1)
	}

	// Without a wake word the bot still listens, for /transcribe; it just
	// never wakes itself.
	wake, err := audio.LoadWakeWord(envOr(ONNXRUNTIME_LIB_KEY, defaultOnnxruntimeLib), envOr(WAKE_DIR_KEY, defaultWakeDir), wakeModel)
	if err != nil {
		slog.Warn("wake word disabled", slog.Any("err", err))
	}

	threshold := defaultWakeThreshold
	if given, err := strconv.ParseFloat(os.Getenv(WAKE_THRESHOLD_KEY), 32); err == nil {
		threshold = given
	}

	client, err := discordbot.Start(ctx, brain, DISCORD_KEY, discordbot.Listening{Wake: wake, Threshold: float32(threshold)})

	if err != nil {
		slog.Error("error while starting discord bot", slog.Any("err", err))
		os.Exit(1)
	}
	defer client.Close(ctx)

	slog.Info("model-citizen is now running. Press CTRL-C to exit.")
	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-s
}

func envOr(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
