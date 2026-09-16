package music

import (
	"sync"
	"time"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/audio"
)

type MusicPlayer interface {
	Queue(url string) error
	ClearQueue()
	Stop()
	PlayNext()
	PlayPrevious()
	CacheRetention() int
	CurrentProgress() (Song, time.Duration, bool)
	CloseCurrent() bool
}

type Song struct {
	URL      string
	Title    string
	File     string
	Duration time.Duration

	reader *audio.OpusStream
}

type MusicProvider struct {
	lock           sync.RWMutex
	songs          []Song
	playing        bool
	queuePosition  int
	maxQueueLength int
	cacheRetention int
}
