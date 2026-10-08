package music

import (
	"context"
	"sync"
	"time"

	"github.com/Y2Kwastaken/model-citizen/shared"
)

type MusicDownload struct {
	Id       string
	Label    string
	Duration time.Duration
	Path     string
}

type MusicDownloadService interface {
	Setup(ctx context.Context)
	Search(query string, ctx context.Context) (MusicDownload, error)
}

func NewYoutubeService(downloadPath string, cacheSize int) (MusicDownloadService, error) {
	cache, err := shared.NewCircularQueue[string](cacheSize)
	if err != nil {
		return nil, err
	}

	return &ytdlService{
		downloadPath:  downloadPath,
		cache:         make(map[string]MusicDownload, cacheSize),
		evictionQueue: cache,
		downloading:   make(map[string]*sync.WaitGroup),
	}, nil
}
