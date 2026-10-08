package network

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/Y2Kwastaken/model-citizen/shared/network/gen/modelcitizen/v1"
)

// connection to the llm, safe to share across goroutines
type Client struct {
	conn   *grpc.ClientConn
	chat   pb.ChatServiceClient
	mem    pb.MemoryServiceClient
	tools  pb.ToolServiceClient
	speech pb.SpeechServiceClient
}

// doesn't connect until the first call so a down llm only fails calls
func New(address string) (*Client, error) {
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	return &Client{
		conn:   conn,
		chat:   pb.NewChatServiceClient(conn),
		mem:    pb.NewMemoryServiceClient(conn),
		tools:  pb.NewToolServiceClient(conn),
		speech: pb.NewSpeechServiceClient(conn),
	}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}
