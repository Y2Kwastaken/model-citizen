package server

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Y2Kwastaken/model-citizen/llm/config"
	"github.com/Y2Kwastaken/model-citizen/llm/model"
	pb "github.com/Y2Kwastaken/model-citizen/shared/network/gen/modelcitizen/v1"
	"google.golang.org/grpc"
)

// how long a graceful shutdown may take before open calls are cut off
const shutdownTimeout = 5 * time.Second

func StartGrpc(brain model.LanguageModel, cfg config.Config, address string) error {
	lis, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}

	chat := cfg.Chat
	server := grpc.NewServer()
	pb.RegisterMemoryServiceServer(server, newMemoryServer(brain, chat.HistorySize))
	pb.RegisterChatServiceServer(server, newChatServer(brain, chat.DefaultPersonality, chat.ReplyTimeout))
	pb.RegisterToolServiceServer(server, newToolServer(brain))

	// without [stt] or [tts] their half answers Unimplemented
	speech, err := newSpeechServer(brain, cfg)
	if err != nil {
		return err
	}
	pb.RegisterSpeechServiceServer(server, speech)
	if !cfg.Stt.Enabled() {
		slog.Info("no [stt] in the llm config, transcription is off")
	}
	if !cfg.Tts.Enabled() {
		slog.Info("no [tts] in the llm config, speech is off")
	}

	go func() {
		if err := server.Serve(lis); err != nil {
			slog.Error("grpc server stopped", slog.Any("error", err))
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	// lets calls already running finish, but the bot's tool stream never ends
	// on its own so anything still open after the timeout is cut off
	stopped := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(shutdownTimeout):
		server.Stop()
	}
	return nil
}
