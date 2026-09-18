package audio

import (
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
)

// UtteranceGap is the pause that separates two utterances by one speaker.
const UtteranceGap = 600 * time.Millisecond

// nearMiss is the score from which a non-trigger is worth a log line, so a
// threshold can be tuned from what real speech actually scores.
const nearMiss = 0.1

const utteranceGap = SampleRate * 6 / 10 // UtteranceGap in samples

// Listener hears everyone in a voice channel. It keeps the last window of
// each speaker's audio and runs the wake word over it, so a trigger can be
// answered with what was said before it, by whom, and when.
//
// It implements voice.OpusFrameReceiver, which disgo drives from its own
// receive goroutine, so every field is behind the mutex.
type Listener struct {
	wake      *WakeWord // nil listens without detecting
	window    time.Duration
	threshold float32
	debounce  time.Duration
	now       func() time.Time

	mu          sync.Mutex
	speakers    map[snowflake.ID]*speaker
	closed      bool
	lastTrigger time.Time
	triggers    chan Trigger
}

// speaker is one person's stream.
type speaker struct {
	decoder   *Decoder
	dec       decimator
	detector  *Detector
	segments  []segment // oldest first, trimmed to the window
	lastHeard time.Time
	lastSeq   uint16    // RTP sequence of the last packet, to see what never arrived
	peak      float32   // best sub-threshold score since it was last logged
	peakAt    time.Time // when the peak was last logged
}

// segment is one packet: when it arrived, the pause the sender skipped
// before it, and its audio.
type segment struct {
	at      time.Time
	silence int
	pcm     []int16
}

// Trigger is the wake word being heard.
type Trigger struct {
	User  snowflake.ID
	At    time.Time
	Score float32
}

// Utterance is one run of speech by one person, pauses filled.
type Utterance struct {
	User  snowflake.ID
	Start time.Time
	PCM   []int16 // SampleRate mono
}

func (u Utterance) Duration() time.Duration {
	return time.Duration(len(u.PCM)) * time.Second / SampleRate
}

// NewListener keeps window of audio per speaker and fires a trigger when a
// detector scores at least threshold, no more than once per debounce.
func NewListener(wake *WakeWord, window time.Duration, threshold float32, debounce time.Duration) *Listener {
	return &Listener{
		wake:      wake,
		window:    window,
		threshold: threshold,
		debounce:  debounce,
		now:       time.Now,
		speakers:  make(map[snowflake.ID]*speaker),
		triggers:  make(chan Trigger, 4),
	}
}

// Triggers delivers wake word hits. It is closed by Close.
func (l *Listener) Triggers() <-chan Trigger {
	return l.triggers
}

func (l *Listener) ReceiveOpusFrame(userID snowflake.ID, packet *voice.Packet) error {
	// disgo maps an SSRC it has not seen a Speaking event for to user 0;
	// that audio has no owner to attribute it to.
	if userID == 0 {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}

	s, ok := l.speakers[userID]
	if !ok {
		decoder, err := NewDecoder()
		if err != nil {
			return err
		}
		s = &speaker{decoder: decoder}
		if l.wake != nil {
			s.detector = l.wake.NewDetector()
		}
		l.speakers[userID] = s
	}

	frame, silence, err := s.decoder.Decode(packet)
	if err != nil {
		slog.Debug("decoding opus frame", slog.Any("err", err))
		return nil
	}

	now := l.now()
	// An utterance starts after a pause. The sequence gap says how many
	// packets the sender numbered that never got here: five is the silence
	// tail DAVE rejects at the end of the last spurt; more than that is the
	// start of this one being lost before it reached us.
	if silence >= utteranceGap && len(s.segments) > 0 {
		slog.Info("utterance start",
			slog.String("user_id", userID.String()),
			slog.Duration("pause", time.Duration(silence)*time.Second/SampleRate),
			slog.Int("packets_missing", int(packet.Sequence-s.lastSeq)-1),
		)
	}
	s.lastSeq = packet.Sequence
	s.lastHeard = now
	s.segments = append(s.segments, segment{at: now, silence: silence, pcm: frame})
	cutoff := now.Add(-l.window)
	for len(s.segments) > 0 && s.segments[0].at.Before(cutoff) {
		s.segments = s.segments[1:]
	}

	if s.detector == nil {
		return nil
	}

	// The detector hears the pause too, or words either side of it run
	// together. Past one scoring window it is all silence anyway.
	fill := min(silence, scoreWindow*wakeChunk*decimation)
	score, err := s.detector.Feed(s.dec.Downsample(append(make([]int16, fill), frame...)))
	if err != nil {
		return err
	}
	if score >= l.threshold {
		s.peak, s.peakAt = 0, now // the run-up to a trigger is not a near miss
		if now.Sub(l.lastTrigger) >= l.debounce {
			l.lastTrigger = now
			select {
			case l.triggers <- Trigger{User: userID, At: now, Score: score}:
			default:
				slog.Warn("wake word trigger dropped, nobody is listening")
			}
		}
		return nil
	}

	// A near miss is logged once a second at most, at its peak, so the
	// threshold can be judged against what speech really scores.
	s.peak = max(s.peak, score)
	if s.peak >= nearMiss && now.Sub(s.peakAt) >= time.Second {
		slog.Info("wake word near miss",
			slog.String("user_id", userID.String()),
			slog.Float64("score", float64(s.peak)),
			slog.Float64("threshold", float64(l.threshold)),
		)
		s.peak, s.peakAt = 0, now
	}
	return nil
}

// CleanupUser forgets a speaker who left.
func (l *Listener) CleanupUser(userID snowflake.ID) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.speakers, userID)
}

// Close stops listening and closes the trigger channel. disgo calls it when
// the voice connection drops.
func (l *Listener) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.closed {
		l.closed = true
		close(l.triggers)
	}
}

// LastHeard is when user's most recent packet arrived.
func (l *Listener) LastHeard(userID snowflake.ID) (time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, ok := l.speakers[userID]
	if !ok {
		return time.Time{}, false
	}
	return s.lastHeard, true
}

// Context is everything said in the window ending at until, one Utterance
// per run of speech, oldest first across all speakers.
func (l *Listener) Context(until time.Time, window time.Duration) []Utterance {
	l.mu.Lock()
	defer l.mu.Unlock()

	from := until.Add(-window)
	var out []Utterance
	for user, s := range l.speakers {
		current := -1
		for _, seg := range s.segments {
			if seg.at.Before(from) || seg.at.After(until) {
				continue
			}
			if current < 0 || seg.silence >= utteranceGap {
				out = append(out, Utterance{User: user, Start: seg.at})
				current = len(out) - 1
			} else {
				out[current].PCM = append(out[current].PCM, make([]int16, seg.silence)...)
			}
			out[current].PCM = append(out[current].PCM, seg.pcm...)
		}
	}

	slices.SortFunc(out, func(a, b Utterance) int { return a.Start.Compare(b.Start) })
	return out
}
