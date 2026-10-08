package server

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Y2Kwastaken/model-citizen/llm/model"
	"github.com/Y2Kwastaken/model-citizen/llm/util"
	pb "github.com/Y2Kwastaken/model-citizen/shared/network/gen/modelcitizen/v1"
)

// chatServer serves pb.ChatService, replying to the history already stored at
// the channel. The bot stores the user's message with MemoryService.Memorize
// first, using the decimal channel id as MemorySource.where
type chatServer struct {
	pb.UnimplementedChatServiceServer

	brain model.LanguageModel
	// personality a guild gets the first time it chats
	defaultPersonality string
	// how long one reply may take, chat.reply_timeout in llm.toml
	replyTimeout time.Duration

	// guilds that already had their personality applied, so a later switch
	// isn't overwritten by the default
	glock  sync.Mutex
	guilds map[int64]struct{}
}

func newChatServer(brain model.LanguageModel, defaultPersonality string, replyTimeout time.Duration) *chatServer {
	return &chatServer{
		brain:              brain,
		defaultPersonality: defaultPersonality,
		replyTimeout:       replyTimeout,
		guilds:             make(map[int64]struct{}),
	}
}

func (s *chatServer) Chat(ctx context.Context, req *pb.ChatRequest) (*pb.ChatResponse, error) {
	// the caller's deadline still applies if it is the shorter one
	ctx, cancel := context.WithTimeout(ctx, s.replyTimeout)
	defer cancel()

	s.applyPersonality(req.GetGuildId())

	source := util.Source{
		Id:    req.GetGuildId(),
		Where: channelKey(req.GetChannelId()),
		Who:   req.GetWho(),
	}

	start := time.Now()
	reply, err := s.brain.Chat(ctx, source)
	slog.Info("chat handled", slog.Int64("guild_id", source.Id), slog.Duration("took", time.Since(start)))
	if err != nil {
		// the caller hanging up or running out of time keeps its own code
		if ctx.Err() != nil {
			return nil, status.FromContextError(ctx.Err()).Err()
		}
		return nil, status.Errorf(codes.Unavailable, "chat: %v", err)
	}

	return &pb.ChatResponse{Reply: reply}, nil
}

// sets the default personality the first time a guild is seen
func (s *chatServer) applyPersonality(guild int64) {
	s.glock.Lock()
	defer s.glock.Unlock()

	if _, ok := s.guilds[guild]; ok {
		return
	}
	if s.brain.SystemMemory(guild, s.defaultPersonality) {
		s.guilds[guild] = struct{}{}
	}
}

// the memory sub id for a channel, must match the MemorySource.where the bot
// sends (source in discord-bot/brain/brain.go)
func channelKey(channel int64) string {
	return strconv.FormatInt(channel, 10)
}
