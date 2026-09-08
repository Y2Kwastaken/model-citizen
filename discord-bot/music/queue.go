package music

import (
	"fmt"
	"slices"
	"time"
)

// Queue operations for MusicProvider.
//
// Every method takes provider.lock for its whole body: a write lock when it
// mutates, a read lock when it only inspects. sync.RWMutex is not reentrant, so
// none of these call each other -- shared logic lives in the unlocked helpers
// at the bottom of this file.

// NewMusicProvider returns an empty queue. A maxQueueLength of zero or less
// means unbounded.
func NewMusicProvider(maxQueueLength, cacheRetention int) *MusicProvider {
	return &MusicProvider{
		maxQueueLength: maxQueueLength,
		cacheRetention: cacheRetention,
	}
}

// Queue appends a Song to the end of the queue.
func (provider *MusicProvider) Queue(url string) error {
	provider.lock.Lock()
	defer provider.lock.Unlock()

	if provider.maxQueueLength > 0 && len(provider.songs) >= provider.maxQueueLength {
		return fmt.Errorf("queue is full (%d songs)", provider.maxQueueLength)
	}

	provider.songs = append(provider.songs, Song{URL: url})
	return nil
}

// ClearQueue empties the queue and resets the position, closing any readers
// that were opened so their ffmpeg processes are released.
//
// Stop playback before calling this: closing the reader the audio sender is
// currently reading makes every later poll fail on a closed pipe.
func (provider *MusicProvider) ClearQueue() {
	provider.lock.Lock()
	defer provider.lock.Unlock()

	for i := range provider.songs {
		provider.songs[i].closeReader()
	}

	provider.songs = nil
	provider.queuePosition = 0
}

// Remove deletes the Song at index, closing its reader if one was opened.
func (provider *MusicProvider) Remove(index int) error {
	provider.lock.Lock()
	defer provider.lock.Unlock()

	if index < 0 || index >= len(provider.songs) {
		return fmt.Errorf("no Song at index %d", index)
	}

	provider.songs[index].closeReader()
	provider.songs = slices.Delete(provider.songs, index, index+1)

	// Keep pointing at the same Song when something earlier is removed.
	if provider.queuePosition > index {
		provider.queuePosition--
	}
	return nil
}

// Current returns the Song at the queue position. The bool is false when the
// queue is empty or the position has run off the end.
func (provider *MusicProvider) Current() (Song, bool) {
	provider.lock.RLock()
	defer provider.lock.RUnlock()

	return provider.at(provider.queuePosition)
}

// Advance moves to the next Song and returns it. It reports false, and leaves
// the position untouched, when the queue is already on the last Song.
func (provider *MusicProvider) Advance() (Song, bool) {
	provider.lock.Lock()
	defer provider.lock.Unlock()

	next, ok := provider.at(provider.queuePosition + 1)
	if !ok {
		return Song{}, false
	}

	provider.queuePosition++
	return next, true
}

// Complete marks the current song finished and moves past it, whether or not
// another follows. The bool reports whether a next song was there.
//
// Unlike Advance this moves even at the end, leaving the position one past the
// last song. That position is not a valid song -- Current reports false -- but
// it is exactly the index the next queued song will occupy, so playback picks
// up naturally instead of replaying the final track.
func (provider *MusicProvider) Complete() (Song, bool) {
	provider.lock.Lock()
	defer provider.lock.Unlock()

	if provider.queuePosition < len(provider.songs) {
		provider.queuePosition++
	}
	return provider.at(provider.queuePosition)
}

// Rewind moves to the previous Song and returns it. It reports false, and
// leaves the position untouched, when already at the start.
func (provider *MusicProvider) Rewind() (Song, bool) {
	provider.lock.Lock()
	defer provider.lock.Unlock()

	previous, ok := provider.at(provider.queuePosition - 1)
	if !ok {
		return Song{}, false
	}

	provider.queuePosition--
	return previous, true
}

func (provider *MusicProvider) CloseCurrent() bool {
	provider.lock.Lock()
	defer provider.lock.Unlock()

	if _, ok := provider.at(provider.queuePosition); !ok {
		return false
	}
	if provider.songs[provider.queuePosition].reader == nil {
		return false
	}
	provider.songs[provider.queuePosition].closeReader()
	return true
}

func (provider *MusicProvider) CurrentProgress() (Song, time.Duration, bool) {
	provider.lock.RLock()
	defer provider.lock.RUnlock()

	song, ok := provider.at(provider.queuePosition)
	if !ok || song.reader == nil {
		return Song{}, 0, false
	}
	return song, song.reader.Elapsed(), true
}

// SetReader attaches an opened reader to a Song, closing whatever it replaces.
func (provider *MusicProvider) SetReader(index int, reader *FriendlyOpusReader) error {
	provider.lock.Lock()
	defer provider.lock.Unlock()

	if index < 0 || index >= len(provider.songs) {
		return fmt.Errorf("no Song at index %d", index)
	}

	if provider.songs[index].reader != reader {
		provider.songs[index].closeReader()
	}
	provider.songs[index].reader = reader
	return nil
}

// QueueLength returns how many songs are queued.
func (provider *MusicProvider) QueueLength() int {
	provider.lock.RLock()
	defer provider.lock.RUnlock()

	return len(provider.songs)
}

// Position returns the index of the current Song.
func (provider *MusicProvider) Position() int {
	provider.lock.RLock()
	defer provider.lock.RUnlock()

	return provider.queuePosition
}

// Songs returns a copy of the queue, safe to read after the lock is released.
func (provider *MusicProvider) Songs() []Song {
	provider.lock.RLock()
	defer provider.lock.RUnlock()

	return slices.Clone(provider.songs)
}

// Playing reports whether playback is currently running.
func (provider *MusicProvider) Playing() bool {
	provider.lock.RLock()
	defer provider.lock.RUnlock()

	return provider.playing
}

// SetPlaying records whether playback is currently running.
func (provider *MusicProvider) SetPlaying(playing bool) {
	provider.lock.Lock()
	defer provider.lock.Unlock()

	provider.playing = playing
}

// CacheRetention returns how many cached files to keep.
func (provider *MusicProvider) CacheRetention() int {
	provider.lock.RLock()
	defer provider.lock.RUnlock()

	return provider.cacheRetention
}

// at returns the Song at index if it exists. The caller must hold the lock.
func (provider *MusicProvider) at(index int) (Song, bool) {
	if index < 0 || index >= len(provider.songs) {
		return Song{}, false
	}
	return provider.songs[index], true
}

// closeReader releases a Song's reader, if it has one. Safe to call more than
// once -- FriendlyOpusReader.Close is guarded by a sync.Once.
func (s *Song) closeReader() {
	if s.reader != nil {
		s.reader.Close()
		s.reader = nil
	}
}
