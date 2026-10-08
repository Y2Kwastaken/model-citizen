package network

import (
	"context"
	"strconv"
	"time"

	pb "github.com/Y2Kwastaken/model-citizen/shared/network/gen/modelcitizen/v1"
	"github.com/disgoorg/snowflake/v2"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (c *Client) Remember(ctx context.Context, guild, channel snowflake.ID, sender, content string, at time.Time) error {
	_, err := c.mem.Memorize(ctx, &pb.MemorizeRequest{
		Source: source(guild, channel),
		Memory: &pb.Memory{
			CreatedAt: timestamppb.New(at),
			Sender:    sender,
			Content:   content,
			Owner:     pb.MemoryOwner_MEMORY_OWNER_OTHER,
		},
	})
	return err
}

// conversion from guild and channel into memory source
func source(guild, channel snowflake.ID) *pb.MemorySource {
	return &pb.MemorySource{
		GuildId: int64(guild),
		Where:   strconv.FormatInt(int64(channel), 10),
	}
}
