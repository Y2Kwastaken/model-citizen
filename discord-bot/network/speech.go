package network

import (
	"context"

	pb "github.com/Y2Kwastaken/model-citizen/shared/network/gen/modelcitizen/v1"
	"github.com/disgoorg/snowflake/v2"
)

// says text in the voice of the guild's personality, any format ffmpeg can read
func (c *Client) Speak(ctx context.Context, guild snowflake.ID, text string) ([]byte, error) {
	resp, err := c.speech.Speak(ctx, &pb.SpeakRequest{GuildId: int64(guild), Text: text})
	if err != nil {
		return nil, err
	}
	return resp.GetAudio(), nil
}

// turns a 16 kHz mono wav into text
func (c *Client) Transcribe(ctx context.Context, wav []byte) (string, error) {
	resp, err := c.speech.Transcribe(ctx, &pb.TranscribeRequest{Audio: wav})
	if err != nil {
		return "", err
	}
	return resp.GetText(), nil
}
