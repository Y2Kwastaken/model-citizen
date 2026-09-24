package discordbot

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/audio"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/music"
)

// tunes set once by Start, from config/music.json
var tunes Music

// One Downloader serves every guild: the cache directory is shared, so a second
// instance would mean two eviction policies fighting over the same files.
// sync.OnceValues builds it on first use and reuses it (or the error) after.
var getDownloader = sync.OnceValues(func() (*music.Downloader, error) {
	return music.NewDownloader(music.CacheDir, tunes.CacheRetention)
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

	song, err := rewindCurrent(event.Client(), guild, event.User().ID)
	if err != nil {
		return replyEphemeral(event, err.Error())
	}

	return reply(event, "Replaying "+playbackLabel(song))
}

// rewindCurrent restarts the song playing in guild, if user is in voice with
// the bot. The error, when there is one, is worded for whoever asked.
func rewindCurrent(client *bot.Client, guild snowflake.ID, user snowflake.ID) (music.Song, error) {
	if err := requireSharedVoice(client, guild, user); err != nil {
		return music.Song{}, err
	}

	player, ok := existingPlayer(guild)
	if !ok || !player.Playing() {
		return music.Song{}, errors.New("Nothing is playing.")
	}

	song, ok := player.Rewind()
	if !ok {
		return music.Song{}, errors.New("Nothing is playing.")
	}

	return song, nil
}

func handleSkip(_ discord.SlashCommandInteractionData, event *handler.CommandEvent) error {
	guild, ok := guildID(event)
	if !ok {
		return replyEphemeral(event, "This command only works in a server.")
	}

	song, err := skipCurrent(event.Client(), guild, event.User().ID)
	if err != nil {
		return replyEphemeral(event, err.Error())
	}

	return reply(event, "Skipped "+playbackLabel(song))
}

// skipCurrent ends the song playing in guild, if user is in voice with the bot.
// The error, when there is one, is worded for whoever asked.
func skipCurrent(client *bot.Client, guild snowflake.ID, user snowflake.ID) (music.Song, error) {
	if err := requireSharedVoice(client, guild, user); err != nil {
		return music.Song{}, err
	}

	player, ok := existingPlayer(guild)
	if !ok || !player.Playing() {
		return music.Song{}, errors.New("Nothing is playing.")
	}

	song, hasSong := player.Current()
	if !player.CloseCurrent() || !hasSong {
		return music.Song{}, errors.New("Nothing is playing.")
	}

	return song, nil
}

func handlePlay(data discord.SlashCommandInteractionData, event *handler.CommandEvent) error {
	guild, ok := guildID(event)
	if !ok {
		return replyEphemeral(event, "This command only works in a server.")
	}

	// The cheap checks run here so their answers can stay ephemeral; a
	// deferred response cannot be made ephemeral after the fact.
	client := event.Client()
	if _, _, err := voiceTarget(client, guild, event.User().ID); err != nil {
		return replyEphemeral(event, err.Error())
	}
	query := strings.TrimSpace(data.String("song"))
	if query == "" {
		return replyEphemeral(event, "Give me a link, or something to search for.")
	}

	if err := event.DeferCreateMessage(false); err != nil {
		return err
	}

	// enqueue may join voice, which blocks on the handshake, so like handleJoin
	// it runs off the gateway goroutine.
	go func() {
		ctx := context.WithoutCancel(event.Ctx)
		queued, err := enqueue(ctx, client, guild, event.User().ID, query)
		if err != nil {
			editResponse(event, err.Error())
			return
		}

		if queued.player.Playing() {
			editResponse(event, "Queued "+queued.label)
			return
		}

		startPlaybackAsync(ctx, guild, queued, func(song music.Song, err error) {
			if err != nil {
				editResponse(event, "Couldn't play that.")
				return
			}
			editResponse(event, "Playing "+playbackLabel(song))
		})
	}()

	return nil
}

// playQueued queues query and starts it if nothing is playing. The bool is
// whether playback started. May block on the voice handshake.
func playQueued(ctx context.Context, client *bot.Client, guild snowflake.ID, user snowflake.ID, query string) (*queued, bool, error) {
	queued, err := enqueue(ctx, client, guild, user, query)
	if err != nil {
		return nil, false, err
	}

	if queued.player.Playing() {
		return queued, false, nil
	}

	startPlaybackAsync(ctx, guild, queued, func(song music.Song, err error) {
		if err == nil {
			slog.Info("started playback",
				slog.String("guild_id", guild.String()),
				slog.String("song", playbackLabel(song)),
			)
		}
	})
	return queued, true, nil
}

// queued is a song that made it onto a guild's queue, with what the caller
// needs to start it if nothing is playing.
type queued struct {
	conn   voice.Conn
	player *music.MusicProvider
	target string
	label  string
}

// enqueue puts query on guild's queue, joining user's voice channel first if
// the bot is not in one. It may block on that handshake, so it must not run on
// the gateway goroutine. The error, when there is one, is worded for whoever
// asked.
func enqueue(ctx context.Context, client *bot.Client, guild snowflake.ID, user snowflake.ID, query string) (*queued, error) {
	conn, err := ensureVoice(ctx, client, guild, user)
	if err != nil {
		return nil, err
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("Give me a link, or something to search for.")
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

	player := playerFor(guild)
	if err := player.Queue(target); err != nil {
		return nil, err
	}

	return &queued{conn: conn, player: player, target: target, label: label}, nil
}

// startPlaybackAsync starts the head of the queue in the background and calls
// report once it is playing or has failed. The download outlives parent's
// cancellation on purpose: the caller has usually already answered by then.
func startPlaybackAsync(parent context.Context, guild snowflake.ID, queued *queued, report func(music.Song, error)) {
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), tunes.PlaybackTimeout)
		defer cancel()

		song, reader, err := startPlayback(ctx, queued.conn, queued.player)
		if err != nil {
			slog.Error("starting playback",
				slog.String("guild_id", guild.String()),
				slog.String("target", queued.target),
				slog.Any("err", err),
			)
			report(song, err)
			return
		}

		// Nothing else notices when this track ends, so hand it to a watcher.
		go advanceOnFinish(queued.conn, queued.player, reader, guild)

		report(song, nil)
	}()
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
// goroutine. See handleJoin for why.
func startPlayback(ctx context.Context, conn voice.Conn, player *music.MusicProvider) (music.Song, *audio.OpusStream, error) {
	downloader, err := getDownloader()
	if err != nil {
		return music.Song{}, nil, err
	}

	// Lazy fetch: downloads only if this song has no cached file yet.
	song, err := player.EnsureCurrent(ctx, downloader)
	if err != nil {
		return music.Song{}, nil, err
	}

	// Spawns ffmpeg, wraps its PCM output in a mixer so the bot can speak
	// over the track, and that in an opus encoder. If the bot is mid-line
	// the track goes under the line on the line's own stream instead, which
	// is already the connection's; a new stream would cut the line off.
	pcm, err := audio.DecodeFile(song.File)
	if err != nil {
		return music.Song{}, nil, err
	}
	reader, adopted := adoptStream(conn.GuildID(), pcm)
	if !adopted {
		mixer := audio.NewMixer(pcm)
		if reader, err = audio.NewOpusStream(mixer, mixer); err != nil {
			_ = mixer.Close()
			return music.Song{}, nil, err
		}
		setMixer(conn.GuildID(), mixer, reader, false)
	}

	// Hand the reader to the queue so ClearQueue and Remove can close it --
	// nothing in disgo ever will.
	if err := player.SetReader(player.Position(), reader); err != nil {
		reader.Close()
		return music.Song{}, nil, err
	}

	// The entire disgo playback API. Audio starts on the next 20ms tick.
	if !adopted {
		conn.SetOpusFrameProvider(reader)
	}
	player.SetPlaying(true)

	// Warm the cache for what comes next while this song plays.
	player.Prefetch(ctx, downloader, tunes.PrefetchAhead)

	return song, reader, nil
}

// advanceOnFinish keeps a guild playing after the current track ends.
//
// disgo gives no completion signal -- its sender polls forever and sends
// silence past the end of a stream -- so this waits on the reader's Done
// channel instead. Done also fires when a reader is closed by /leave or a
// queue clear, which is how this goroutine learns to exit.
func advanceOnFinish(conn voice.Conn, player *music.MusicProvider, reader *audio.OpusStream, guild snowflake.ID) {
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
func advanceToPlayable(conn voice.Conn, player *music.MusicProvider, guild snowflake.ID) (*audio.OpusStream, bool) {
	for {
		if _, ok := player.Complete(); !ok {
			return nil, false
		}

		ctx, cancel := context.WithTimeout(context.Background(), tunes.PlaybackTimeout)
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
	// Nothing is mixing any more, so a line now needs a stream of its own.
	clearMixer(conn.GuildID())
}

func playerFor(guild snowflake.ID) *music.MusicProvider {
	playersMu.Lock()
	defer playersMu.Unlock()

	if existing, ok := players[guild]; ok {
		return existing
	}

	created := music.NewMusicProvider(tunes.MaxQueueLength, tunes.CacheRetention)
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
