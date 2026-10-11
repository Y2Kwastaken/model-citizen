package network

import (
	"context"
	"strconv"
	"time"

	pb "github.com/Y2Kwastaken/model-citizen/shared/network/gen/modelcitizen/v1"
	"github.com/disgoorg/snowflake/v2"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// a line someone said, in text or voice
type Message struct {
	Sender  string
	Content string
	At      time.Time
}

// stores a message the bot isn't replying to in the channel's history.
// Unimplemented means the llm doesn't remember
func (c *Client) Remember(ctx context.Context, guild, channel snowflake.ID, message Message) error {
	_, err := c.mem.Memorize(ctx, &pb.MemorizeRequest{
		Source: source(guild, channel),
		Memory: message.proto(),
	})
	return err
}

func (m Message) proto() *pb.Memory {
	return &pb.Memory{
		CreatedAt: timestamppb.New(m.At),
		Sender:    m.Sender,
		Content:   m.Content,
		Owner:     pb.MemoryOwner_MEMORY_OWNER_OTHER,
	}
}

// conversion from guild and channel into memory source
func source(guild, channel snowflake.ID) *pb.MemorySource {
	return &pb.MemorySource{
		GuildId: int64(guild),
		Where:   strconv.FormatInt(int64(channel), 10),
	}
}
