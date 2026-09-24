package audio

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"sync"
)

// duckedGain is how much of the bed survives while the bot is talking over it.
// Speech loses to music otherwise: a track is mastered loud and a synthesised
// line is not. Set once before any mixer runs.
var DuckedGain float32 = 0.25

// Mixer is a PCM source that plays the bot's speech over whatever else is
// playing.
//
// Discord takes one opus stream per connection, so two things are heard at
// once only by summing their samples before the encoder. NewOpusStream reads
// from any io.Reader, which is where this sits -- between ffmpeg and the
// encoder, with the music none the wiser.
//
// A Mixer with no bed is how the bot talks when nothing is playing: it ends as
// soon as it has nothing left to say, and the stream ends with it -- unless a
// bed arrives first (SetBed), in which case the line carries on over it and
// the mixer becomes the music's.
type Mixer struct {
	// bed and its state belong to whoever is reading, which is only ever the
	// one producer goroutine inside OpusStream.
	bed     io.Reader
	bedDone bool
	frame   []byte
	spoken  []byte
	pos     int

	lock     sync.Mutex
	queue    []*speech
	finished bool
	closed   bool
	// a bed handed over mid-line, taken up at the next frame
	pending io.Reader
	// frames built so far, which is where a pending bed will start
	frames int64
}

// speech is one clip waiting its turn.
type speech struct {
	pcm  io.ReadCloser
	done chan struct{}
}

// NewMixer wraps bed, which may be nil when nothing is playing.
func NewMixer(bed io.Reader) *Mixer {
	return &Mixer{
		bed:     bed,
		bedDone: bed == nil,
		frame:   make([]byte, FrameBytes),
		spoken:  make([]byte, FrameBytes),
		pos:     FrameBytes,
	}
}

// Say queues pcm to play over the bed and closes it once it has been read.
// The channel closes when the clip has been mixed in full.
//
// It reports false if the mixer is already spent, in which case nothing will
// ever read the clip and the caller should give it a stream of its own.
func (mixer *Mixer) Say(pcm io.ReadCloser) (<-chan struct{}, bool) {
	mixer.lock.Lock()
	defer mixer.lock.Unlock()

	if mixer.finished || mixer.closed {
		return nil, false
	}

	line := &speech{pcm: pcm, done: make(chan struct{})}
	mixer.queue = append(mixer.queue, line)
	return line.done, true
}

// SetBed starts playing bed under whatever is being said, from the next
// frame. It reports the frame the bed starts at, so an OpusStream that was
// carrying a line alone can count the bed's own time (SetOrigin), and false
// if the mixer is already spent, in which case the bed needs a mixer of its
// own.
//
// This is how a song that starts while the bot is mid-sentence goes under
// the sentence instead of cutting it off: the line's stream is already on
// the connection, so the song joins it rather than replacing it.
func (mixer *Mixer) SetBed(bed io.Reader) (int64, bool) {
	mixer.lock.Lock()
	defer mixer.lock.Unlock()

	if mixer.finished || mixer.closed || mixer.pending != nil || !mixer.bedDone {
		return 0, false
	}
	mixer.pending = bed
	return mixer.frames, true
}

// Read hands out the mixed audio one frame at a time.
func (mixer *Mixer) Read(p []byte) (int, error) {
	for mixer.pos >= len(mixer.frame) {
		if err := mixer.fill(); err != nil {
			return 0, err
		}
		mixer.lock.Lock()
		mixer.frames++
		mixer.lock.Unlock()
	}

	n := copy(p, mixer.frame[mixer.pos:])
	mixer.pos += n
	return n, nil
}

// Close releases the bed and anything still queued, so a stopped stream does
// not leave an ffmpeg running for a line nobody will hear.
func (mixer *Mixer) Close() error {
	mixer.lock.Lock()
	queued := mixer.queue
	mixer.queue = nil
	alreadyClosed := mixer.closed
	mixer.closed = true
	mixer.lock.Unlock()

	if alreadyClosed {
		return nil
	}

	for _, line := range queued {
		_ = line.pcm.Close()
		close(line.done)
	}

	if closer, ok := mixer.pending.(io.Closer); ok {
		_ = closer.Close()
	}
	if closer, ok := mixer.bed.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// fill builds the next frame: the bed, ducked, with whatever is being said on
// top of it. It reports io.EOF once the bed has ended and there is nothing
// left to say.
func (mixer *Mixer) fill() error {
	for {
		mixer.takeBed()
		mixer.readBed()

		said, ok := mixer.readSpeech()
		if ok {
			if !mixer.bedDone {
				mixdown(mixer.frame, said)
			} else {
				copy(mixer.frame, said)
			}
			mixer.pos = 0
			return nil
		}

		if !mixer.bedDone {
			mixer.pos = 0
			return nil
		}

		// Nothing playing and nothing to say. Claim the end under the lock so
		// a line queued at this exact moment either lands before the claim and
		// is played, or is refused and given a stream of its own.
		mixer.lock.Lock()
		if len(mixer.queue) > 0 {
			mixer.lock.Unlock()
			continue
		}
		mixer.finished = true
		mixer.lock.Unlock()
		return io.EOF
	}
}

// takeBed picks up a bed handed over since the last frame.
func (mixer *Mixer) takeBed() {
	mixer.lock.Lock()
	defer mixer.lock.Unlock()

	if mixer.pending != nil {
		mixer.bed, mixer.pending = mixer.pending, nil
		mixer.bedDone = false
	}
}

// readBed fills the frame from the bed, padding with silence once it ends.
func (mixer *Mixer) readBed() {
	if mixer.bedDone {
		clear(mixer.frame)
		return
	}

	n, err := io.ReadFull(mixer.bed, mixer.frame)
	if err != nil {
		// A short final read still plays; the rest of the frame is silence.
		clear(mixer.frame[n:])
		mixer.bedDone = true
	}
}

// readSpeech fills the speech frame from the clip being said, retiring it once
// it runs out. It reports false when nothing is being said.
func (mixer *Mixer) readSpeech() ([]byte, bool) {
	line := mixer.current()
	if line == nil {
		return nil, false
	}

	n, err := io.ReadFull(line.pcm, mixer.spoken)
	if err == nil {
		return mixer.spoken, true
	}

	clear(mixer.spoken[n:])
	mixer.retire(line)

	// A clip that ended exactly on a frame boundary has nothing left to play.
	if n == 0 && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, false
	}
	return mixer.spoken, true
}

func (mixer *Mixer) current() *speech {
	mixer.lock.Lock()
	defer mixer.lock.Unlock()

	if len(mixer.queue) == 0 {
		return nil
	}
	return mixer.queue[0]
}

// retire drops a finished clip and wakes whoever was waiting on it.
func (mixer *Mixer) retire(line *speech) {
	mixer.lock.Lock()
	if len(mixer.queue) > 0 && mixer.queue[0] == line {
		mixer.queue = mixer.queue[1:]
	}
	mixer.lock.Unlock()

	_ = line.pcm.Close()
	close(line.done)
}

// mixdown sums said into bed in place, ducking the bed as it goes. In place
// because this runs 50 times a second and a frame of stereo 48kHz is not
// worth allocating that often.
func mixdown(bed []byte, said []byte) {
	for i := 0; i+1 < len(bed) && i+1 < len(said); i += 2 {
		under := float32(int16(binary.LittleEndian.Uint16(bed[i:]))) * DuckedGain
		over := int32(int16(binary.LittleEndian.Uint16(said[i:])))
		binary.LittleEndian.PutUint16(bed[i:], uint16(clamp(int32(under)+over)))
	}
}

// clamp keeps a sum inside the sample range: wrapping would turn a loud
// moment into a click.
func clamp(sample int32) int16 {
	switch {
	case sample > math.MaxInt16:
		return math.MaxInt16
	case sample < math.MinInt16:
		return math.MinInt16
	default:
		return int16(sample)
	}
}
