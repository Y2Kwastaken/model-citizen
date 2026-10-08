package network

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	pb "github.com/Y2Kwastaken/model-citizen/shared/network/gen/modelcitizen/v1"
	"github.com/disgoorg/snowflake/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

type Call struct {
	Guild     snowflake.ID
	Channel   string
	Who       string
	Arguments string
}

type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
	Run         func(ctx context.Context, call Call) (string, error)
}

func (c *Client) RegisterTools(ctx context.Context, tools []Tool) {
	byName := make(map[string]Tool, len(tools))
	for _, tool := range tools {
		byName[tool.Name] = tool
	}

	for ctx.Err() == nil {
		if err := c.serveOnce(ctx, tools, byName); err != nil {
			slog.Warn("tool stream ended, retrying", slog.Any("error", err))
		}
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
}

func (c *Client) serveOnce(ctx context.Context, tools []Tool, byName map[string]Tool) error {
	// the llm forgets tools when it restarts so they are registered on every connect
	for _, tool := range tools {
		req := &pb.RegisterToolRequest{Name: tool.Name, Description: tool.Description}
		if tool.Parameters != nil {
			params, err := structpb.NewStruct(tool.Parameters)
			if err != nil {
				return err
			}
			req.Parameters = params
		}
		if _, err := c.tools.RegisterTool(ctx, req); err != nil && status.Code(err) != codes.AlreadyExists {
			return err
		}
	}

	stream, err := c.tools.ToolCalls(ctx)
	if err != nil {
		return err
	}

	// Send isn't safe from several goroutines
	var slock sync.Mutex
	for {
		resp, err := stream.Recv()
		if err != nil {
			return err
		}

		call := resp.GetCall()
		go func() {
			result := &pb.ToolResult{Id: call.GetId()}
			if out, err := run(ctx, byName, call); err != nil {
				result.Outcome = &pb.ToolResult_Error{Error: err.Error()}
			} else {
				result.Outcome = &pb.ToolResult_Output{Output: out}
			}

			slock.Lock()
			defer slock.Unlock()
			stream.Send(&pb.ToolCallsRequest{Result: result})
		}()
	}
}

func run(ctx context.Context, byName map[string]Tool, call *pb.ToolCall) (string, error) {
	tool, ok := byName[call.GetName()]
	if !ok {
		return "", fmt.Errorf("unknown tool %s", call.GetName())
	}
	return tool.Run(ctx, Call{
		Guild:     snowflake.ID(call.GetGuildId()),
		Channel:   call.GetWhere(),
		Who:       call.GetWho(),
		Arguments: call.GetArguments(),
	})
}
