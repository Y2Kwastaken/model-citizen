// claude authored, kept separate from the hand written audio code

package audio

import (
	"log/slog"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/wake"
	sharedaudio "github.com/Y2Kwastaken/model-citizen/shared/audio"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
)

const (
	// scores at or above this are logged as near misses to help tune the threshold
	nearMiss = 0.1
	// past the detector's 1.28 s window a longer pause is all silence anyway
	maxPauseFill = sampleRate * 128 / 100
	// a pause this long separates two utterances by the same person
	utteranceGap = 600 * time.Millisecond
)

// a wake word heard in the voice channel
type Wake struct {
	User  snowflake.ID
	At    time.Time
	Score float32
}

// one person's run of speech at 16 kHz, short pauses kept
type Utterance struct {
	User  snowflake.ID
	Start time.Time
	PCM   []int16
}

func (u Utterance) Duration() time.Duration {
	return time.Duration(len(u.PCM)) * time.Second / wake.SampleRate
}

// the receiving side of the OpusMixer, it hears everyone in the channel and
// runs the wake word over each speaker. disgo calls ReceiveOpusFrame from its
// own goroutine so it never blocks
type OpusListener struct {
	model     *wake.Model
	threshold float32
	debounce  time.Duration
	// how much of each speaker's audio is kept
	keep time.Duration
	// frames at or above this many dBFS are speech, quieter is silence
	quietLevel float64

	lock     sync.Mutex
	speakers map[snowflake.ID]*speaker
	wakes    chan Wake
}

type speaker struct {
	decoder     *decoder
	downsampler *sharedaudio.Downsampler
	detector    *wake.Detector
	lastWake    time.Time
	lastHeard   time.Time
	// discord keeps sending packets through background noise, so speech is
	// told apart from silence by how loud it is
	lastLoud time.Time
	// loudest and quietest frame in the current second, logged to tune quietLevel
	levelHigh float64
	levelLow  float64
	levelAt   time.Time
	// oldest first, trimmed to keep
	segments []segment
	// best score under the threshold since it was last logged
	peak   float32
	peakAt time.Time
}

// one packet of audio at 16 kHz
type segment struct {
	at time.Time
	// how long the speaker was quiet before it
	pause time.Duration
	// that pause as samples, capped at maxPauseFill
	quiet []int16
	pcm   []int16
}

func NewOpusListener(model *wake.Model, threshold float32, debounce time.Duration, keep time.Duration, quietLevel float64) *OpusListener {
	return &OpusListener{
		model:      model,
		threshold:  threshold,
		debounce:   debounce,
		keep:       keep,
		quietLevel: quietLevel,
		speakers:   make(map[snowflake.ID]*speaker),
		wakes:      make(chan Wake, 4),
	}
}

// wake words as they're heard, never closed since the listener lives as long as its guild
func (l *OpusListener) Wakes() <-chan Wake {
	return l.wakes
}

func (l *OpusListener) ReceiveOpusFrame(userID snowflake.ID, packet *voice.Packet) error {
	// disgo hasn't matched this audio to a user yet
	if userID == 0 {
		return nil
	}

	l.lock.Lock()
	defer l.lock.Unlock()

	s, err := l.speaker(userID)
	if err != nil {
		return err
	}

	pcm, silence, err := s.decoder.decode(packet)
	if err != nil {
		// one bad packet shouldn't stop listening
		slog.Debug("decoding opus packet", slog.String("user_id", userID.String()), slog.Any("error", err))
		return nil
	}

	// the pause is heard too or the words either side of it run together
	quiet := s.downsampler.Downsample(make([]int16, min(silence, maxPauseFill)))
	speech := s.downsampler.Downsample(pcm)

	now := time.Now()
	s.lastHeard = now
	l.trackLevel(userID, s, level(pcm), now)
	s.segments = append(s.segments, segment{
		at:    now,
		pause: time.Duration(silence) * time.Second / sampleRate,
		quiet: quiet,
		pcm:   speech,
	})
	cutoff := now.Add(-l.keep)
	for len(s.segments) > 0 && s.segments[0].at.Before(cutoff) {
		s.segments = s.segments[1:]
	}

	if _, err := s.detector.Feed(quiet); err != nil {
		return err
	}
	score, err := s.detector.Feed(speech)
	if err != nil {
		return err
	}

	if score >= l.threshold {
		s.peak = 0
		if now.Sub(s.lastWake) >= l.debounce {
			s.lastWake = now
			select {
			case l.wakes <- Wake{User: userID, At: now, Score: score}:
			default:
				slog.Warn("wake word dropped, nothing is reading wakes", slog.String("user_id", userID.String()))
			}
		}
		return nil
	}

	// logged at most once a second, at the highest score
	s.peak = max(s.peak, score)
	if s.peak >= nearMiss && now.Sub(s.peakAt) >= time.Second {
		slog.Debug("wake word near miss",
			slog.String("user_id", userID.String()),
			slog.Float64("score", float64(s.peak)),
			slog.Float64("threshold", float64(l.threshold)),
		)
		s.peak, s.peakAt = 0, now
	}

	return nil
}

// fetches a speaker, creating one the first time they're heard. the caller holds the lock
func (l *OpusListener) speaker(userID snowflake.ID) (*speaker, error) {
	if s, ok := l.speakers[userID]; ok {
		return s, nil
	}

	dec, err := newDecoder()
	if err != nil {
		return nil, err
	}

	s := &speaker{
		decoder:     dec,
		downsampler: sharedaudio.NewDownsampler(),
		detector:    l.model.NewDetector(),
	}
	l.speakers[userID] = s
	return s, nil
}

// notes when a speaker was last loud enough to be talking, and logs their
// level range once a second
func (l *OpusListener) trackLevel(userID snowflake.ID, s *speaker, dBFS float64, now time.Time) {
	if dBFS >= l.quietLevel {
		s.lastLoud = now
	}

	if s.levelAt.IsZero() {
		s.levelHigh, s.levelLow, s.levelAt = dBFS, dBFS, now
	}
	s.levelHigh = max(s.levelHigh, dBFS)
	s.levelLow = min(s.levelLow, dBFS)
	if now.Sub(s.levelAt) >= time.Second {
		slog.Debug("voice level",
			slog.String("user_id", userID.String()),
			slog.Float64("loudest_dbfs", math.Round(s.levelHigh)),
			slog.Float64("quietest_dbfs", math.Round(s.levelLow)),
			slog.Float64("quiet_level", l.quietLevel),
		)
		s.levelAt = time.Time{}
	}
}

// a frame's rms in dBFS, 0 is full scale and silence is -100
func level(pcm []int16) float64 {
	if len(pcm) == 0 {
		return -100
	}

	var sum float64
	for _, sample := range pcm {
		normal := float64(sample) / 32768
		sum += normal * normal
	}
	rms := math.Sqrt(sum / float64(len(pcm)))
	if rms == 0 {
		return -100
	}
	return max(20*math.Log10(rms), -100)
}

// when a speaker was last loud enough to be talking
func (l *OpusListener) LastLoud(userID snowflake.ID) (time.Time, bool) {
	l.lock.Lock()
	defer l.lock.Unlock()

	s, ok := l.speakers[userID]
	if !ok {
		return time.Time{}, false
	}
	return s.lastLoud, true
}

// when a speaker was last heard
func (l *OpusListener) LastHeard(userID snowflake.ID) (time.Time, bool) {
	l.lock.Lock()
	defer l.lock.Unlock()

	s, ok := l.speakers[userID]
	if !ok {
		return time.Time{}, false
	}
	return s.lastHeard, true
}

// everything said between from and until, one Utterance per run of speech,
// oldest first across every speaker
func (l *OpusListener) Utterances(from time.Time, until time.Time) []Utterance {
	l.lock.Lock()
	defer l.lock.Unlock()

	var out []Utterance
	for user, s := range l.speakers {
		current := -1
		for _, seg := range s.segments {
			if seg.at.Before(from) || seg.at.After(until) {
				continue
			}

			if current < 0 || seg.pause >= utteranceGap {
				out = append(out, Utterance{User: user, Start: seg.at})
				current = len(out) - 1
			} else {
				out[current].PCM = append(out[current].PCM, seg.quiet...)
			}
			out[current].PCM = append(out[current].PCM, seg.pcm...)
		}
	}

	slices.SortFunc(out, func(a, b Utterance) int {
		return a.Start.Compare(b.Start)
	})
	return out
}

// forgets a speaker who left the channel
func (l *OpusListener) CleanupUser(userID snowflake.ID) {
	l.lock.Lock()
	defer l.lock.Unlock()
	delete(l.speakers, userID)
}

// forgets every speaker, the listener itself is reused on the next join
func (l *OpusListener) Close() {
	l.lock.Lock()
	defer l.lock.Unlock()
	clear(l.speakers)
}
