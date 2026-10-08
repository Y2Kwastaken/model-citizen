package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Y2Kwastaken/model-citizen/llm/model"
	"github.com/Y2Kwastaken/model-citizen/llm/tools"
	"github.com/Y2Kwastaken/model-citizen/llm/util"
	pb "github.com/Y2Kwastaken/model-citizen/shared/network/gen/modelcitizen/v1"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

// toolServer serves pb.ToolService, adding tools the bot registers to the
// brain's tool set and running them over the bot's ToolCalls stream
type toolServer struct {
	pb.UnimplementedToolServiceServer

	brain model.LanguageModel

	// the bot's open stream, nil while no bot is connected
	block sync.Mutex
	bot   *botStream

	// ids for calls, unique for the life of the process
	nextID atomic.Uint64
}

func newToolServer(brain model.LanguageModel) *toolServer {
	return &toolServer{brain: brain}
}

func (s *toolServer) RegisterTool(ctx context.Context, req *pb.RegisterToolRequest) (*pb.RegisterToolResponse, error) {
	name := req.GetName()
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	function := shared.FunctionDefinitionParam{
		Name: name,
		// an empty object schema when the tool takes no arguments
		Parameters: shared.FunctionParameters{"type": "object", "properties": map[string]any{}},
	}
	if req.GetDescription() != "" {
		function.Description = openai.String(req.GetDescription())
	}
	if req.GetParameters() != nil {
		function.Parameters = req.GetParameters().AsMap()
	}

	err := s.brain.ToolSet().Add(tools.Tool{
		Function: function,
		Executor: s.remoteExecutor(name),
	})
	if err != nil {
		// a reconnecting bot registers again, it can treat this as success
		return nil, status.Errorf(codes.AlreadyExists, "registering tool: %v", err)
	}

	return &pb.RegisterToolResponse{}, nil
}

// lives as long as the bot keeps the stream open, the newest stream gets new calls
func (s *toolServer) ToolCalls(stream pb.ToolService_ToolCallsServer) error {
	bot := &botStream{
		stream:  stream,
		pending: make(map[string]chan *pb.ToolResult),
		done:    make(chan struct{}),
	}

	s.block.Lock()
	s.bot = bot
	s.block.Unlock()

	defer func() {
		s.block.Lock()
		if s.bot == bot {
			s.bot = nil
		}
		s.block.Unlock()
		bot.close()
	}()

	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		if result := req.GetResult(); result != nil {
			bot.deliver(result)
		}
	}
}

// sends calls to the connected bot and waits for its result
func (s *toolServer) remoteExecutor(name string) tools.ToolExecutor {
	return func(ctx context.Context, src util.Source, args string) (string, error) {
		s.block.Lock()
		bot := s.bot
		s.block.Unlock()
		if bot == nil {
			return "", fmt.Errorf("tool %s is unavailable, the bot isn't connected", name)
		}

		result, err := bot.call(ctx, &pb.ToolCall{
			Id:        strconv.FormatUint(s.nextID.Add(1), 10),
			Name:      name,
			Arguments: args,
			GuildId:   src.Id,
			Where:     src.Where,
			Who:       src.Who,
		})
		if err != nil {
			return "", err
		}

		if outcome, ok := result.GetOutcome().(*pb.ToolResult_Error); ok {
			return "", errors.New(outcome.Error)
		}
		return result.GetOutput(), nil
	}
}

// one open ToolCalls stream and the calls waiting on it
type botStream struct {
	// Send isn't safe from several goroutines, and must not run after the
	// handler returns, so closed is checked under the same lock
	slock  sync.Mutex
	stream pb.ToolService_ToolCallsServer
	closed bool

	plock   sync.Mutex
	pending map[string]chan *pb.ToolResult

	// closed once the stream ends, wakes every waiting call
	done chan struct{}
}

func (b *botStream) call(ctx context.Context, call *pb.ToolCall) (*pb.ToolResult, error) {
	// buffered so deliver never blocks on a call that already gave up
	result := make(chan *pb.ToolResult, 1)
	b.plock.Lock()
	b.pending[call.Id] = result
	b.plock.Unlock()

	defer func() {
		b.plock.Lock()
		delete(b.pending, call.Id)
		b.plock.Unlock()
	}()

	if err := b.send(call); err != nil {
		return nil, err
	}

	select {
	case r := <-result:
		return r, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.done:
		return nil, errors.New("the bot disconnected before the tool finished")
	}
}

func (b *botStream) send(call *pb.ToolCall) error {
	b.slock.Lock()
	defer b.slock.Unlock()
	if b.closed {
		return errors.New("the bot disconnected")
	}
	return b.stream.Send(&pb.ToolCallsResponse{Call: call})
}

// hands a result to the call waiting on it, results nobody waits on are dropped
func (b *botStream) deliver(result *pb.ToolResult) {
	b.plock.Lock()
	waiting, ok := b.pending[result.GetId()]
	delete(b.pending, result.GetId())
	b.plock.Unlock()

	if ok {
		waiting <- result
	}
}

func (b *botStream) close() {
	b.slock.Lock()
	b.closed = true
	b.slock.Unlock()
	close(b.done)
}
