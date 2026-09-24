package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type MusicSettings struct {
	// zero or less means unlimited
	MaxQueueLength int
	// cached songs kept on disk, zero or less keeps everything
	CacheRetention int
	// upcoming songs downloaded in the background
	PrefetchAhead int
	// bounds a download plus ffmpeg startup
	PlaybackTimeout time.Duration
	// how loud songs play, 0 to 1
	Volume float32
	// music volume while the bot talks over it, 0 to 1
	DuckedGain float32
}

type musicSettingsRaw struct {
	MaxQueueLength         int     `json:"max_queue_length"`
	CacheRetention         int     `json:"cache_retention"`
	PrefetchAhead          int     `json:"prefetch_ahead"`
	PlaybackTimeoutSeconds int     `json:"playback_timeout_seconds"`
	Volume                 float32 `json:"volume"`
	DuckedGain             float32 `json:"ducked_gain"`
}

func NewMusicSettings(config string) (MusicSettings, error) {
	data, err := os.ReadFile(config)
	if err != nil {
		return MusicSettings{}, err
	}

	var raw musicSettingsRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return MusicSettings{}, err
	}

	if raw.PrefetchAhead < 0 {
		return MusicSettings{}, fmt.Errorf("prefetch_ahead must be at least 0, got %d", raw.PrefetchAhead)
	}
	if raw.PlaybackTimeoutSeconds <= 0 {
		return MusicSettings{}, fmt.Errorf("playback_timeout_seconds must be above 0, got %d", raw.PlaybackTimeoutSeconds)
	}
	if raw.Volume <= 0 || raw.Volume > 1 {
		return MusicSettings{}, fmt.Errorf("volume must be above 0 and at most 1, got %v", raw.Volume)
	}
	if raw.DuckedGain < 0 || raw.DuckedGain > 1 {
		return MusicSettings{}, fmt.Errorf("ducked_gain must be between 0 and 1, got %v", raw.DuckedGain)
	}

	return MusicSettings{
		MaxQueueLength:  raw.MaxQueueLength,
		CacheRetention:  raw.CacheRetention,
		PrefetchAhead:   raw.PrefetchAhead,
		PlaybackTimeout: time.Duration(raw.PlaybackTimeoutSeconds) * time.Second,
		Volume:          raw.Volume,
		DuckedGain:      raw.DuckedGain,
	}, nil
}
