package network

import (
	"context"

	pb "github.com/Y2Kwastaken/model-citizen/shared/network/gen/modelcitizen/v1"
	"github.com/disgoorg/snowflake/v2"
)

func (c *Client) Reply(ctx context.Context, guild, channel, user snowflake.ID) (string, error) {
	resp, err := c.chat.Chat(ctx, &pb.ChatRequest{
		GuildId:   int64(guild),
		ChannelId: int64(channel),
		Who:       user.String(),
	})
	if err != nil {
		return "", err
	}
	return resp.GetReply(), nil
}
