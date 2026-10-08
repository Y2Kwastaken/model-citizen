// claude authored, kept separate from the hand written audio code

package audio

import (
	"context"
	"errors"
	"log/slog"
	"net"

	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
)

// disgo's default receiver, but packets that fail to read are logged at debug.
// the first packets of every talk spurt arrive before discord says who sent
// them, so they can't be DAVE decrypted and the default logs each as an error
type audioReceiver struct {
	logger       *slog.Logger
	opusReceiver voice.OpusFrameReceiver
	conn         voice.Conn
	ctx          context.Context
	cancel       context.CancelFunc
}

// an AudioReceiverCreateFunc for voice.WithConnAudioReceiverCreateFunc
func NewAudioReceiver(logger *slog.Logger, opusReceiver voice.OpusFrameReceiver, conn voice.Conn) voice.AudioReceiver {
	ctx, cancel := context.WithCancel(context.Background())
	return &audioReceiver{
		logger:       logger,
		opusReceiver: opusReceiver,
		conn:         conn,
		ctx:          ctx,
		cancel:       cancel,
	}
}

func (r *audioReceiver) Open() {
	go r.receive()
}

func (r *audioReceiver) receive() {
	for r.ctx.Err() == nil {
		packet, err := r.conn.UDP().ReadPacket()
		if errors.Is(err, net.ErrClosed) {
			r.Close()
			return
		}
		if err != nil {
			r.logger.Debug("skipping unreadable packet", slog.Any("err", err))
			continue
		}

		if err := r.opusReceiver.ReceiveOpusFrame(r.conn.UserIDBySSRC(packet.SSRC), packet); err != nil {
			r.logger.Error("error while receiving opus frame", slog.Any("err", err))
		}
	}
}

func (r *audioReceiver) CleanupUser(userID snowflake.ID) {
	r.opusReceiver.CleanupUser(userID)
}

func (r *audioReceiver) Close() {
	r.cancel()
	r.opusReceiver.Close()
}
