package music

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"sync"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/audio"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/config"
	"github.com/Y2Kwastaken/model-citizen/shared"
	saudio "github.com/Y2Kwastaken/model-citizen/shared/audio"
)

type MusicPlayer interface {
	// queues a song and executes a download if need be this should be
	// run off of critical paths
	Queue(song string, ctx context.Context) error
	Skip() error
	Stop() error
	Playing() (MusicDownload, bool)
	ListQueue() iter.Seq[MusicDownload]
}

func NewBasicPlayer(service MusicDownloadService, mixer *audio.OpusMixer, cfg config.Music, bufferFrames int) (MusicPlayer, error) {
	queue, err := shared.NewCircularQueue[MusicDownload](cfg.QueueCapacity)
	if err != nil {
		return nil, err
	}

	return &basicPlayer{
		service: service,
		mixer:   mixer,
		queue:   queue,

		volumeDB:     cfg.VolumeDB,
		bufferFrames: bufferFrames,
	}, nil
}

type basicPlayer struct {
	service MusicDownloadService
	mixer   *audio.OpusMixer

	layerId uint8
	playing MusicDownload
	layer   *saudio.MixingLayer

	queue shared.CircularQueue[MusicDownload]
	// held across the fullness check and enqueue so concurrent queues can't
	// overfill and evict the oldest song
	qlock   sync.Mutex
	slock   sync.Mutex
	started bool

	volumeDB     float32
	bufferFrames int
}

func (p *basicPlayer) Queue(song string, ctx context.Context) error {
	// cheap early out so a full queue doesn't cost a download
	if err := p.checkQueueFull(); err != nil {
		return err
	}

	download, err := p.service.Search(song, ctx)
	if err != nil {
		return err
	}

	p.qlock.Lock()
	if err := p.checkQueueFull(); err != nil {
		p.qlock.Unlock()
		return err
	}
	p.queue.Enqueue(download)
	p.qlock.Unlock()

	p.slock.Lock()
	if !p.started {
		go p.start()
	}
	p.started = true
	p.slock.Unlock()
	return nil
}

func (p *basicPlayer) checkQueueFull() error {
	if p.queue.Size() >= p.queue.Capacity() {
		return fmt.Errorf("queue is full (%d songs)", p.queue.Capacity())
	}
	return nil
}

func (p *basicPlayer) Skip() error {
	p.mixer.Unregister(0)
	return nil
}

func (p *basicPlayer) Stop() error {
	_, val := p.queue.Dequeue()
	for val {
		_, val = p.queue.Dequeue()
	}

	p.mixer.Unregister(0)
	return nil
}

func (p *basicPlayer) Playing() (MusicDownload, bool) {
	p.slock.Lock()
	defer p.slock.Unlock()
	if !p.started {
		return MusicDownload{}, false
	}

	return p.playing, true
}

func (p *basicPlayer) ListQueue() iter.Seq[MusicDownload] {
	return p.queue.All()
}

func (p *basicPlayer) start() {
	p.slock.Lock()
	defer p.slock.Unlock()

	download, ok := p.queue.Dequeue()
	if !ok {
		p.started = false
		return
	}

	pipe, err := saudio.WavToPcmStream(download.Path)
	if err != nil {
		slog.Error("starting ffmpeg", slog.Any("error", err))
		go p.start()
		return
	}

	p.layer = saudio.NewMixingLayer(0, 0.5, p.bufferFrames, pipe.Reader, saudio.GainEditor(p.volumeDB))
	layer := p.layer

	err = p.mixer.Register(0, p.layer)
	if err != nil {
		pipe.Close()
		slog.Error("registering mixer", slog.Any("error", err))
		go p.start()
		return
	}

	p.playing = download
	go func() {
		<-layer.Ctx.Done()
		pipe.Close()
		go p.start()
	}()
}
