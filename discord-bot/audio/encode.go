package audio

import (
	"bufio"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"layeh.com/gopus"
)

const (
	maxEncodedFrame = 1400
	bitrate         = 96000

	// bufferedFrames is how far ahead encoding may run, in 20ms frames, and
	// so also how late the bot is heard: a line mixed into this stream joins
	// audio that is already encoded and waiting. 50 frames is one second,
	// which leaves ffmpeg real slack -- it is reading a file off disk, not a
	// network -- without the bot answering into a conversation that has moved
	// on. See TestTheEncoderStaysCloseToPlayback.
	bufferedFrames = 50

	// prefillFrames is how much must be buffered before playback starts.
	// Spawning ffmpeg and filling the first pipe read costs ~100ms, which is
	// five times the 20ms frame budget; priming pays that before the audio
	// sender is watching the clock.
	prefillFrames = 25

	// prefillTimeout bounds the wait for that priming. Exceeding it is not
	// fatal -- playback starts unprimed rather than failing.
	prefillTimeout = 10 * time.Second
)

// OpusStream converts s16le PCM into the opus frames disgo sends.
//
// Reading and encoding happen on their own goroutine, feeding a buffered
// channel. ProvideOpusFrame only receives from that channel, so disgo's 20ms
// deadline is never spent on pipe I/O or opus encoding -- the two things whose
// latency we do not control.
type OpusStream struct {
	closer io.Closer
	once   sync.Once

	// encoder is touched only by the producer goroutine.
	encoder *gopus.Encoder

	frames chan []byte   // producer -> consumer
	cancel chan struct{} // closed by Close, unblocks a parked producer

	primedOnce sync.Once
	primed     chan struct{} // closed once enough frames are buffered

	// time tracking for the reader
	sent atomic.Int64

	// err is written by the producer before it closes frames, and read by the
	// consumer only after observing that close, which orders the two.
	err error

	// done is consumer-side state, so it needs no synchronisation.
	done     bool
	finished chan struct{}
}

func NewOpusStream(src io.Reader, closer io.Closer) (*OpusStream, error) {
	// The encoder is stateful -- opus predicts across frames, so one encoder
	// must serve every frame of the stream.
	encoder, err := gopus.NewEncoder(SampleRate, Channels, gopus.Audio)
	if err != nil {
		return nil, err
	}
	encoder.SetBitrate(bitrate)

	reader := &OpusStream{
		closer:   closer,
		encoder:  encoder,
		frames:   make(chan []byte, bufferedFrames),
		cancel:   make(chan struct{}),
		primed:   make(chan struct{}),
		finished: make(chan struct{}),
	}

	go reader.produce(src)

	// Wait for the buffer to fill a little before handing this to the audio
	// sender. A short track that ends before prefillFrames unblocks this too,
	// because the producer marks primed on its way out.
	select {
	case <-reader.primed:
	case <-time.After(prefillTimeout):
	}

	return reader, nil
}

// Elapsed reports how much audio has actually been sent. Counting frames
// rather than wall clock is what makes this exact: if the producer stalls,
// ProvideOpusFrame blocks and the sender stalls with it, so a clock would
// drift where the frame count does not.
func (reader *OpusStream) Elapsed() time.Duration {
	return time.Duration(reader.sent.Load()) * FrameLength
}

// produce reads PCM, encodes it, and buffers the result until the source ends
// or Close cancels it.
func (reader *OpusStream) produce(src io.Reader) {
	defer close(reader.frames)
	defer reader.markPrimed()

	buffer := bufio.NewReader(src)
	var frame [FrameBytes]byte
	pcm := make([]int16, FrameSize*Channels)
	produced := 0

	for {
		n, err := io.ReadFull(buffer, frame[:])
		if errors.Is(err, io.ErrUnexpectedEOF) {
			// Short final read: pad the rest with silence so the last partial
			// 20ms still plays.
			err = nil
			clear(frame[n:])
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				reader.err = err
			}
			return
		}

		PCMFromBytes(frame[:], pcm)

		// Encode allocates a fresh slice per call, so buffered frames never
		// alias each other.
		encoded, err := reader.encoder.Encode(pcm, FrameSize, maxEncodedFrame)
		if err != nil {
			reader.err = err
			return
		}

		select {
		case reader.frames <- encoded:
		case <-reader.cancel:
			return
		}

		produced++
		if produced >= prefillFrames {
			reader.markPrimed()
		}
	}
}

// ProvideOpusFrame hands disgo the next buffered frame.
//
// It blocks if the producer has fallen behind: a brief pause is better than
// returning nothing, which makes disgo emit silence and drop the speaking flag.
func (reader *OpusStream) ProvideOpusFrame() ([]byte, error) {
	if reader.done {
		return nil, io.EOF
	}

	frame, ok := <-reader.frames
	if ok {
		reader.sent.Add(1)
		return frame, nil
	}

	// The channel is closed AND drained: only now has every produced frame
	// actually been sent. Signalling completion any earlier would cut off
	// however much audio was still buffered.
	reader.done = true
	reader.Close()

	if reader.err != nil {
		return nil, reader.err
	}
	return nil, io.EOF
}

// Close releases the underlying resource and stops the producer. It is safe to
// call repeatedly, which matters because the audio sender keeps polling long
// after the stream ends.
func (reader *OpusStream) Close() {
	reader.once.Do(func() {
		// Unblock a producer parked on a full buffer, then kill the source it
		// is reading, which unblocks one parked in ReadFull.
		close(reader.cancel)
		if reader.closer != nil {
			_ = reader.closer.Close()
		}
		close(reader.finished)
	})
}

// Done returns a channel closed once the stream has been fully played or the
// reader has been closed.
//
// disgo never reports that a track finished -- its sender just keeps polling
// and sending silence -- so this is how a caller learns to move on. It fires
// for an external Close too, so a stop or skip wakes the same waiter.
func (reader *OpusStream) Done() <-chan struct{} {
	return reader.finished
}

func (reader *OpusStream) markPrimed() {
	reader.primedOnce.Do(func() { close(reader.primed) })
}
