package server

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Y2Kwastaken/model-citizen/llm/memory"
	"github.com/Y2Kwastaken/model-citizen/llm/model"
	pb "github.com/Y2Kwastaken/model-citizen/shared/network/gen/modelcitizen/v1"
)

// memoryServer serves pb.MemoryService on top of the brain's memory holders
type memoryServer struct {
	pb.UnimplementedMemoryServiceServer

	brain       model.LanguageModel
	historySize int
}

func newMemoryServer(brain model.LanguageModel, historySize int) *memoryServer {
	return &memoryServer{brain: brain, historySize: historySize}
}

func (s *memoryServer) Memorize(ctx context.Context, req *pb.MemorizeRequest) (*pb.MemorizeResponse, error) {
	source := req.GetSource()
	if source == nil {
		return nil, status.Error(codes.InvalidArgument, "source is required")
	}
	if req.GetMemory() == nil {
		return nil, status.Error(codes.InvalidArgument, "memory is required")
	}

	holder, err := s.holder(source)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "creating memory holder: %v", err)
	}

	if _, _, err := holder.Store(fromProto(req.GetMemory())); err != nil {
		return nil, status.Errorf(codes.Internal, "storing memory: %v", err)
	}
	return &pb.MemorizeResponse{}, nil
}

func (s *memoryServer) Recall(ctx context.Context, req *pb.RecallRequest) (*pb.RecallResponse, error) {
	source := req.GetSource()
	if source == nil {
		return nil, status.Error(codes.InvalidArgument, "source is required")
	}

	// nothing stored there yet is an empty history, not an error
	holder, ok := s.brain.Memory(source.GetGuildId(), source.GetWhere())
	if !ok {
		return &pb.RecallResponse{}, nil
	}

	ordered := holder.Ordered()
	memories := make([]*pb.Memory, len(ordered))
	for i, mem := range ordered {
		memories[i] = toProto(mem)
	}
	return &pb.RecallResponse{Memories: memories}, nil
}

// fetches the holder at the source, creating a short term memory for it on first use
func (s *memoryServer) holder(source *pb.MemorySource) (memory.MemoryHolder, error) {
	id, where := source.GetGuildId(), source.GetWhere()
	if holder, ok := s.brain.Memory(id, where); ok {
		return holder, nil
	}

	holder, err := memory.NewShortTermMemory(s.historySize)
	if err != nil {
		return nil, err
	}

	// another call may have added one first, either way use whatever got stored
	s.brain.AddMemoryHolder(id, where, holder)
	holder, _ = s.brain.Memory(id, where)
	return holder, nil
}

func fromProto(m *pb.Memory) memory.Memory {
	createdAt := time.Now()
	if m.GetCreatedAt() != nil {
		createdAt = m.GetCreatedAt().AsTime()
	}

	var involves []string
	if m.GetSender() != "" {
		involves = []string{m.GetSender()}
	}

	return memory.Memory{
		CreatedAt: createdAt,
		Involves:  involves,
		Content:   m.GetContent(),
		Owner:     ownerFromProto(m.GetOwner()),
	}
}

func toProto(m memory.Memory) *pb.Memory {
	var sender string
	if len(m.Involves) > 0 {
		sender = m.Involves[0]
	}

	return &pb.Memory{
		CreatedAt: timestamppb.New(m.CreatedAt),
		Sender:    sender,
		Content:   m.Content,
		Owner:     ownerToProto(m.Owner),
	}
}

func ownerFromProto(owner pb.MemoryOwner) memory.Identity {
	switch owner {
	case pb.MemoryOwner_MEMORY_OWNER_SELF:
		return memory.SELF
	case pb.MemoryOwner_MEMORY_OWNER_OTHER:
		return memory.OTHER
	default:
		return memory.NONE
	}
}

func ownerToProto(owner memory.Identity) pb.MemoryOwner {
	switch owner {
	case memory.SELF:
		return pb.MemoryOwner_MEMORY_OWNER_SELF
	case memory.OTHER:
		return pb.MemoryOwner_MEMORY_OWNER_OTHER
	default:
		return pb.MemoryOwner_MEMORY_OWNER_UNSPECIFIED
	}
}
