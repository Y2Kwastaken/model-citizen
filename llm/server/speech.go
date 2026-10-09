package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Y2Kwastaken/model-citizen/llm/clients"
	"github.com/Y2Kwastaken/model-citizen/llm/config"
	"github.com/Y2Kwastaken/model-citizen/llm/model"
	"github.com/Y2Kwastaken/model-citizen/shared"
	pb "github.com/Y2Kwastaken/model-citizen/shared/network/gen/modelcitizen/v1"
)

// speechServer serves pb.SpeechService, both directions are optional and
// answer Unimplemented when their section isn't in the config
type speechServer struct {
	pb.UnimplementedSpeechServiceServer

	stt          config.Stt
	transcribers shared.Rotation[clients.Transcriber]

	tts config.Tts
	// voice name to the speakers that can say it
	voices map[string]shared.Rotation[clients.Speaker]

	brain              model.LanguageModel
	defaultPersonality string
	personalities      map[string]config.Personality
}

func newSpeechServer(brain model.LanguageModel, cfg config.Config) (*speechServer, error) {
	s := &speechServer{
		stt:                cfg.Stt,
		tts:                cfg.Tts,
		brain:              brain,
		defaultPersonality: cfg.Chat.DefaultPersonality,
		personalities:      cfg.Chat.Personalities,
	}

	if cfg.Stt.Enabled() {
		transcribers, err := shared.NewRotation(cfg.Stt.Transcribers(), cfg.Stt.Rotation.Policy())
		if err != nil {
			return nil, err
		}
		s.transcribers = transcribers
	}

	if cfg.Tts.Enabled() {
		s.voices = make(map[string]shared.Rotation[clients.Speaker])
		for name, speakers := range cfg.Tts.Speakers() {
			voice, err := shared.NewRotation(speakers, cfg.Tts.Rotation.Policy())
			if err != nil {
				return nil, fmt.Errorf("tts.voices.%s: %w", name, err)
			}
			s.voices[name] = voice
		}
	}

	return s, nil
}

func (s *speechServer) Transcribe(ctx context.Context, req *pb.TranscribeRequest) (*pb.TranscribeResponse, error) {
	if s.transcribers == nil {
		return nil, status.Error(codes.Unimplemented, "no [stt] in the llm config")
	}
	if len(req.GetAudio()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "audio is required")
	}

	ctx, cancel := context.WithTimeout(ctx, s.stt.Timeout)
	defer cancel()

	text, err := failover(ctx, s.transcribers, s.stt.PerClientTimeout, clients.Transcriber.Model,
		func(ctx context.Context, transcriber clients.Transcriber) (string, error) {
			return transcriber.Transcribe(ctx, req.GetAudio(), s.stt.Language)
		})
	if err != nil {
		return nil, err
	}
	return &pb.TranscribeResponse{Text: text}, nil
}

// speaks in the voice of the guild's current personality
func (s *speechServer) Speak(ctx context.Context, req *pb.SpeakRequest) (*pb.SpeakResponse, error) {
	if s.voices == nil {
		return nil, status.Error(codes.Unimplemented, "no [tts] in the llm config")
	}
	if req.GetText() == "" {
		return nil, status.Error(codes.InvalidArgument, "text is required")
	}

	personality, ok := s.brain.SystemMemoryName(req.GetGuildId())
	if !ok {
		personality = s.defaultPersonality
	}
	voice := s.personalities[personality].Voice
	if voice == "" {
		return nil, status.Errorf(codes.FailedPrecondition, "personality %s has no voice", personality)
	}

	ctx, cancel := context.WithTimeout(ctx, s.tts.Timeout)
	defer cancel()

	audio, err := failover(ctx, s.voices[voice], s.tts.PerClientTimeout, clients.Speaker.Voice,
		func(ctx context.Context, speaker clients.Speaker) ([]byte, error) {
			return speaker.Speak(ctx, req.GetText())
		})
	if err != nil {
		return nil, err
	}
	return &pb.SpeakResponse{Audio: audio}, nil
}

// gives each client in the rotation one try, each with perClient to answer.
// a failure benches the client so the next one is current, a success is
// judged on how long it took
func failover[T any, R any](ctx context.Context, rotation shared.Rotation[T], perClient time.Duration, name func(T) string, try func(context.Context, T) (R, error)) (R, error) {
	var none R
	for range rotation.Len() {
		entry := rotation.Current()
		start := time.Now()

		attempt, cancel := context.WithTimeout(ctx, perClient)
		result, err := try(attempt, entry.Value)
		cancel()
		if err == nil {
			took := time.Since(start)
			rotation.Judge(entry, took)
			slog.Debug("speech client answered", slog.String("client", name(entry.Value)), slog.Duration("took", took))
			return result, nil
		}

		// the caller hanging up or running out of time keeps its own code
		if ctx.Err() != nil {
			return none, status.FromContextError(ctx.Err()).Err()
		}

		slog.Warn("speech client failed, trying the next one", slog.String("client", name(entry.Value)), slog.Any("error", err))
		rotation.Fail(entry)
	}

	return none, status.Error(codes.Unavailable, "every client failed")
}
