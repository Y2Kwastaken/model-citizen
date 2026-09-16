package audio

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
	"layeh.com/gopus"
)

// maxSilenceFill caps the pause Decode will reinsert. A timestamp gap longer
// than this is a reordered or bogus packet rather than a pause, so one bad
// header cannot make us allocate minutes of silence.
const maxSilenceFill = 5 * SampleRate

// Decoder turns one speaker's packets into mono PCM at SampleRate.
//
// Discord clients stop transmitting through silence rather than sending quiet
// frames, so the gap between RTP timestamps is the only record that a pause
// happened. Decode reinserts it: left unfilled, the words butt up against each
// other and a model hears a sentence nobody said.
//
// One Decoder serves one stream. Opus predicts across frames, and the gap
// tracking assumes a single timestamp line.
type Decoder struct {
	opus     *gopus.Decoder
	started  bool
	lastTime uint32
}

func NewDecoder() (*Decoder, error) {
	opus, err := gopus.NewDecoder(SampleRate, Channels)
	if err != nil {
		return nil, fmt.Errorf("creating opus decoder: %w", err)
	}
	return &Decoder{opus: opus}, nil
}

// Decode returns the packet's audio as mono, prefixed with silence for any
// pause since the previous packet. The slice is fresh on every call.
func (d *Decoder) Decode(packet *voice.Packet) ([]int16, error) {
	stereo, err := d.opus.Decode(packet.Opus, FrameSize, false)
	if err != nil {
		return nil, err
	}

	// Timestamps count samples, so subtracting the last one and the frame it
	// covered leaves the samples nobody sent. uint32 arithmetic handles the
	// clock wrapping.
	var gap int
	if d.started {
		if elapsed := int(packet.Timestamp - d.lastTime); elapsed > FrameSize && elapsed-FrameSize < maxSilenceFill {
			gap = elapsed - FrameSize
		}
	}
	d.started = true
	d.lastTime = packet.Timestamp

	// Both channels carry the same voice, so mono halves the output and costs
	// nothing a model would have used.
	mono := make([]int16, gap+len(stereo)/Channels)
	for i := range len(stereo) / Channels {
		mono[gap+i] = int16((int32(stereo[i*Channels]) + int32(stereo[i*Channels+1])) / 2)
	}
	return mono, nil
}

// Recorder collects one user's audio off a voice connection.
//
// It implements voice.OpusFrameReceiver, which disgo drives from its own
// receive goroutine, so every field is behind the mutex. Packets before Start
// are dropped; that is how a caller lets a fresh connection settle before the
// window opens.
type Recorder struct {
	target  snowflake.ID
	decoder *Decoder
	limit   int

	mu      sync.Mutex
	started bool
	closed  bool
	pcm     []int16
	frames  int
}

// NewRecorder records target for at most window of audio.
func NewRecorder(target snowflake.ID, window time.Duration) (*Recorder, error) {
	decoder, err := NewDecoder()
	if err != nil {
		return nil, err
	}

	limit := int(window.Seconds() * SampleRate)
	return &Recorder{
		target:  target,
		decoder: decoder,
		limit:   limit,
		pcm:     make([]int16, 0, limit),
	}, nil
}

// Start opens the window.
func (r *Recorder) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started = true
}

// ReceiveOpusFrame decodes one packet into the buffer, ignoring anyone who is
// not the target user.
func (r *Recorder) ReceiveOpusFrame(userID snowflake.ID, packet *voice.Packet) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.started || r.closed || userID != r.target || len(r.pcm) >= r.limit {
		return nil
	}

	mono, err := r.decoder.Decode(packet)
	if err != nil {
		// One malformed packet is not worth abandoning a capture over, and an
		// error returned here only reaches disgo's logger anyway.
		slog.Debug("decoding opus frame", slog.Any("err", err))
		return nil
	}
	r.frames++

	if room := r.limit - len(r.pcm); room < len(mono) {
		mono = mono[:room]
	}
	r.pcm = append(r.pcm, mono...)
	return nil
}

// CleanupUser is a no-op: a capture is short enough that someone leaving
// part-way through just yields a shorter clip.
func (*Recorder) CleanupUser(_ snowflake.ID) {}

// Close stops collection. disgo calls it when the voice connection drops.
func (r *Recorder) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
}

// Stop closes the window and returns what was heard, with the number of
// packets that arrived.
//
// Zero packets is the failure worth reporting separately: it means the
// connection never delivered audio, which is a different problem from the user
// having said nothing.
func (r *Recorder) Stop() (pcm []int16, frames int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return r.pcm, r.frames
}
