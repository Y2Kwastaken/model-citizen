package music

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"sync"

	"layeh.com/gopus"
)

const (
	bytes_per_frame   = 3840
	samples_per_frame = 960
	channels          = 2
	sample_rate       = 48000
	max_encoded_frame = 1400
	bitrate           = 96000
)

type FriendlyOpusReader struct {
	buffer  *bufio.Reader
	closer  io.Closer
	once    sync.Once
	done    bool
	encoder *gopus.Encoder
	frame   [bytes_per_frame]byte
	pcm     []int16
}

func (reader *FriendlyOpusReader) ProvideOpusFrame() ([]byte, error) {
	if reader.done {
		return nil, io.EOF
	}

	n, err := io.ReadFull(reader.buffer, reader.frame[:])
	if errors.Is(err, io.ErrUnexpectedEOF) {
		err = nil
		clear(reader.frame[n:])
	}

	if err != nil {
		// Release ffmpeg here -- nothing in disgo ever calls Close for us. Mark
		// done so later polls take the branch above: Close closes the pipe, so
		// reading it again yields os.ErrClosed, which disgo logs every 20ms.
		reader.done = true
		reader.Close()
		return nil, err
	}

	for i := range reader.pcm {
		reader.pcm[i] = int16(binary.LittleEndian.Uint16(reader.frame[i*2:]))
	}

	return reader.encoder.Encode(reader.pcm, samples_per_frame, max_encoded_frame)
}

func (reader *FriendlyOpusReader) Close() {
	reader.once.Do(func() {
		if reader.closer != nil {
			_ = reader.closer.Close()
		}
	})
}

func NewFriendlyOpusReader(src io.Reader, closer io.Closer) (*FriendlyOpusReader, error) {
	encoder, err := gopus.NewEncoder(sample_rate, channels, gopus.Audio)
	if err != nil {
		return nil, err
	}
	encoder.SetBitrate(bitrate)

	return &FriendlyOpusReader{
		buffer:  bufio.NewReader(src),
		closer:  closer,
		encoder: encoder,
		pcm:     make([]int16, samples_per_frame*channels),
	}, nil
}
