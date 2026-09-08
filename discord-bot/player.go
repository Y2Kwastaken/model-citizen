package discordbot

import (
	"sync"

	"github.com/disgoorg/snowflake/v2"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/music"
)

const (
	maxQueueLength = 100
	cacheRetention = 25
)

var (
	playersMu sync.Mutex
	players   = map[snowflake.ID]*music.MusicProvider{}
)

func playerFor(guild snowflake.ID) *music.MusicProvider {
	playersMu.Lock()
	defer playersMu.Unlock()

	if existing, ok := players[guild]; ok {
		return existing
	}

	created := music.NewMusicProvider(maxQueueLength, cacheRetention)
	players[guild] = created
	return created
}

func existingPlayer(guild snowflake.ID) (*music.MusicProvider, bool) {
	playersMu.Lock()
	defer playersMu.Unlock()

	existing, ok := players[guild]
	return existing, ok
}

func removePlayer(guild snowflake.ID) {
	playersMu.Lock()
	existing, ok := players[guild]
	delete(players, guild)
	playersMu.Unlock()

	if ok && existing != nil {
		existing.ClearQueue()
	}
}
