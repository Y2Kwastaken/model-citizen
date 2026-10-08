package state

import (
	"sync"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/audio"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/config"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/network"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/wake"
	sharedaudio "github.com/Y2Kwastaken/model-citizen/shared/audio"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/music"
	"github.com/disgoorg/snowflake/v2"
)

// we leak services on guild leave right now I'm actually probably okay with that currently
type GlobalServices struct {
	Config config.Config
	Music  music.MusicDownloadService
	// llm connection set in main
	Brain *network.Client
	// wake word models set in main, nil when they failed to load
	Wake *wake.Model
	// answers a guild's wake words, set in main
	OnWake        func(guild snowflake.ID, listener *audio.OpusListener)
	glock         sync.RWMutex
	guildServices map[snowflake.ID]GuildServices
}

func InitGlobalServices(cfg config.Config) (GlobalServices, error) {
	ytService, err := music.NewYoutubeService(cfg.Music.Cache.Directory, cfg.Music.Cache.MaxTracks)
	if err != nil {
		return GlobalServices{}, err
	}

	return GlobalServices{
		Config:        cfg,
		Music:         ytService,
		guildServices: make(map[snowflake.ID]GuildServices),
	}, nil
}

func (g *GlobalServices) RegisterGuild(guild snowflake.ID) error {
	mixer, err := audio.NewOpusMixer(sharedaudio.NewGeneralMixer(960))
	if err != nil {
		return err
	}

	player, err := music.NewBasicPlayer(g.Music, mixer, g.Config.Music, g.Config.Voice.LayerBufferFrames)
	if err != nil {
		return err
	}

	// no wake word means the bot joins voice without listening
	var listener *audio.OpusListener
	if g.Wake != nil {
		listen := g.Config.Listen
		listener = audio.NewOpusListener(g.Wake, listen.Threshold, listen.Debounce, listen.Window, listen.QuietLevel)
		go g.OnWake(guild, listener)
	}

	guildServices := GuildServices{
		Mixer:    mixer,
		Listener: listener,
		Player:   player,
	}

	g.glock.Lock()
	defer g.glock.Unlock()
	g.guildServices[guild] = guildServices
	return nil
}

func (g *GlobalServices) GetGuild(guild snowflake.ID) (GuildServices, bool) {
	g.glock.RLock()
	defer g.glock.RUnlock()

	service, ok := g.guildServices[guild]
	return service, ok
}

type GuildServices struct {
	Mixer *audio.OpusMixer
	// nil when the wake word isn't loaded
	Listener *audio.OpusListener
	Player   music.MusicPlayer
}
