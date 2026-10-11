package network

import (
	"context"

	pb "github.com/Y2Kwastaken/model-citizen/shared/network/gen/modelcitizen/v1"
	"github.com/disgoorg/snowflake/v2"
)

// replies to messages, what's new in the channel since the last reply
func (c *Client) Reply(ctx context.Context, guild, channel, user snowflake.ID, messages []Message) (string, error) {
	sent := make([]*pb.Memory, len(messages))
	for i, message := range messages {
		sent[i] = message.proto()
	}

	resp, err := c.chat.Chat(ctx, &pb.ChatRequest{
		GuildId:   int64(guild),
		ChannelId: int64(channel),
		Who:       user.String(),
		Messages:  sent,
	})
	if err != nil {
		return "", err
	}
	return resp.GetReply(), nil
}
