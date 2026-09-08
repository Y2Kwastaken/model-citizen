package discordbot

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/music"
)

const (
	maxQueueLength = 100
	cacheRetention = 25

	// prefetchAhead is how many upcoming songs to download in the background.
	prefetchAhead = 2

	// playbackTimeout bounds a download plus ffmpeg startup.
	playbackTimeout = 2 * time.Minute
)

// One Downloader serves every guild: the cache directory is shared, so a second
// instance would mean two eviction policies fighting over the same files.
// sync.OnceValues builds it on first use and reuses it (or the error) after.
var getDownloader = sync.OnceValues(func() (*music.Downloader, error) {
	return music.NewDownloader(music.CacheDir, cacheRetention)
})

var (
	playersMu sync.Mutex
	players   = map[snowflake.ID]*music.MusicProvider{}
)

func handleRewind(_ discord.SlashCommandInteractionData, event *handler.CommandEvent) error {
	guild, ok := guildID(event)
	if !ok {
		return replyEphemeral(event, "This command only works in a server.")
	}

	client := event.Client()
	botChannel, userChannel, inSame := ensureBotInUserVoice(client, guild, event.User().ID)
	if botChannel == nil {
		return replyEphemeral(event, "bot must be connected to a voice channel to do this.")
	}
	if userChannel == nil {
		return replyEphemeral(event, "you must be connected to a voice channel to do this.")
	}
	if !inSame {
		return replyEphemeral(event, "you must be in the same channel as the bot to do this.")
	}

	player, ok := existingPlayer(guild)
	if !ok || !player.Playing() {
		return replyEphemeral(event, "Nothing is playing.")
	}

	song, ok := player.Rewind()
	if !ok {
		return replyEphemeral(event, "Nothing is playing.")
	}

	return reply(event, "Replaying "+playbackLabel(song))
}

func handleSkip(_ discord.SlashCommandInteractionData, event *handler.CommandEvent) error {
	guild, ok := guildID(event)
	if !ok {
		return replyEphemeral(event, "This command only works in a server.")
	}

	client := event.Client()
	botChannel, userChannel, inSame := ensureBotInUserVoice(client, guild, event.User().ID)
	if botChannel == nil {
		return replyEphemeral(event, "bot must be connected to a voice channel to do this.")
	}
	if userChannel == nil {
		return replyEphemeral(event, "you must be connected to a voice channel to do this.")
	}
	if !inSame {
		return replyEphemeral(event, "you must be in the same channel as the bot to do this.")
	}

	player, ok := existingPlayer(guild)
	if !ok || !player.Playing() {
		return replyEphemeral(event, "Nothing is playing.")
	}

	song, hasSong := player.Current()
	if !player.CloseCurrent() || !hasSong {
		return replyEphemeral(event, "Nothing is playing.")
	}

	return reply(event, "Skipped "+playbackLabel(song))
}

func handlePlay(data discord.SlashCommandInteractionData, event *handler.CommandEvent) error {
	guild, ok := guildID(event)
	if !ok {
		return replyEphemeral(event, "This command only works in a server.")
	}

	client := event.Client()
	botChannel, userChannel, inSame := ensureBotInUserVoice(client, guild, event.User().ID)
	if botChannel == nil {
		return replyEphemeral(event, "bot must be connected to a voice channel to do this.")
	}
	if userChannel == nil {
		return replyEphemeral(event, "you must be connected to a voice channel to do this.")
	}
	if !inSame {
		return replyEphemeral(event, "you must be in the same channel as the bot to do this.")
	}

	query := strings.TrimSpace(data.String("song"))
	if query == "" {
		return replyEphemeral(event, "Give me a link, or something to search for.")
	}

	// Anything that isn't a link becomes a yt-dlp search target, resolved lazily
	// at download time -- so a search costs nothing here. The prefix goes on
	// unconditionally: the target lands in yt-dlp's argv, so a query starting
	// with "-" would otherwise be read as a flag rather than a search term.
	target, label := query, query
	if !validURL(query) {
		target = "ytsearch1:" + query
		label = "search: " + query
	}

	conn := client.VoiceManager.GetConn(guild)
	if conn == nil || conn.ChannelID() == nil {
		return replyEphemeral(event, "I've lost my voice connection -- try /leave then /join.")
	}

	player := playerFor(guild)
	if err := player.Queue(target); err != nil {
		return replyEphemeral(event, err.Error())
	}

	if player.Playing() {
		return replyEphemeral(event, "Queued "+label)
	}

	if err := event.DeferCreateMessage(false); err != nil {
		return err
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(event.Ctx), playbackTimeout)
		defer cancel()

		song, reader, err := startPlayback(ctx, conn, player)
		if err != nil {
			slog.Error("starting playback",
				slog.String("guild_id", guild.String()),
				slog.String("target", target),
				slog.Any("err", err),
			)
			editResponse(event, "Couldn't play that.")
			return
		}

		// Nothing else notices when this track ends, so hand it to a watcher.
		go advanceOnFinish(conn, player, reader, guild)

		editResponse(event, "Playing "+playbackLabel(song))
	}()

	return nil
}

func playbackLabel(song music.Song) string {
	if song.Title != "" {
		return song.Title
	}
	return song.URL
}

// startPlayback turns the queue's current song into audio on the voice conn.
//
// The three lines that matter are at the bottom: download, decode, hand to
// disgo. Everything disgo needs after that -- the 20ms clock, RTP framing,
// speaking flags, encryption -- it does itself.
//
// This blocks on the download, so it must not run on the gateway event
// goroutine. See connectVoice for why.
func startPlayback(ctx context.Context, conn voice.Conn, player *music.MusicProvider) (music.Song, *music.FriendlyOpusReader, error) {
	downloader, err := getDownloader()
	if err != nil {
		return music.Song{}, nil, err
	}

	// Lazy fetch: downloads only if this song has no cached file yet.
	song, err := player.EnsureCurrent(ctx, downloader)
	if err != nil {
		return music.Song{}, nil, err
	}

	// Spawns ffmpeg and wraps its PCM output in an opus encoder.
	reader, err := music.TranslateFile(song.File)
	if err != nil {
		return music.Song{}, nil, err
	}

	// Hand the reader to the queue so ClearQueue and Remove can close it --
	// nothing in disgo ever will.
	if err := player.SetReader(player.Position(), reader); err != nil {
		reader.Close()
		return music.Song{}, nil, err
	}

	// The entire disgo playback API. Audio starts on the next 20ms tick.
	conn.SetOpusFrameProvider(reader)
	player.SetPlaying(true)

	// Warm the cache for what comes next while this song plays.
	player.Prefetch(ctx, downloader, prefetchAhead)

	return song, reader, nil
}

// advanceOnFinish keeps a guild playing after the current track ends.
//
// disgo gives no completion signal -- its sender polls forever and sends
// silence past the end of a stream -- so this waits on the reader's Done
// channel instead. Done also fires when a reader is closed by /leave or a
// queue clear, which is how this goroutine learns to exit.
func advanceOnFinish(conn voice.Conn, player *music.MusicProvider, reader *music.FriendlyOpusReader, guild snowflake.ID) {
	for {
		<-reader.Done()

		// The bot may have been disconnected while that track played.
		if conn.ChannelID() == nil {
			player.SetPlaying(false)
			return
		}

		// Walk forward until something starts. A song that fails to download is
		// skipped rather than wedging the rest of the queue.
		next, ok := advanceToPlayable(conn, player, guild)
		if !ok {
			stopPlayback(conn, player)
			return
		}
		reader = next
	}
}

// advanceToPlayable steps past the finished song and starts the first later one
// that plays, reporting false when the queue runs out.
func advanceToPlayable(conn voice.Conn, player *music.MusicProvider, guild snowflake.ID) (*music.FriendlyOpusReader, bool) {
	for {
		if _, ok := player.Complete(); !ok {
			return nil, false
		}

		ctx, cancel := context.WithTimeout(context.Background(), playbackTimeout)
		_, reader, err := startPlayback(ctx, conn, player)
		cancel()

		if err != nil {
			slog.Error("advancing to next song",
				slog.String("guild_id", guild.String()),
				slog.Any("err", err),
			)
			continue
		}
		return reader, true
	}
}

// stopPlayback silences the conn without touching the queue. A nil provider is
// the supported way to stop: the sender keeps running but sends nothing.
func stopPlayback(conn voice.Conn, player *music.MusicProvider) {
	conn.SetOpusFrameProvider(nil)
	player.SetPlaying(false)
}

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
