package music

import (
	"context"
	"fmt"
	"log/slog"
)

// Queue-to-downloader glue. These live apart from queue.go so the queue itself
// stays a pure data structure, and apart from download.go so the downloader
// knows nothing about queues.

// EnsureCurrent returns the current Song with a cached file, downloading it
// first if necessary. This is the lazy half: nothing is fetched until the
// player actually needs it.
//
// It blocks on the download, so call it off the gateway event goroutine.
func (provider *MusicProvider) EnsureCurrent(ctx context.Context, downloader *Downloader) (Song, error) {
	current, ok := provider.Current()
	if !ok {
		return Song{}, fmt.Errorf("queue is empty")
	}
	if current.File != "" {
		return current, nil
	}

	download, err := downloader.Get(ctx, current.URL)
	if err != nil {
		return Song{}, err
	}

	provider.recordDownload(current.URL, download)

	// Re-read so the caller gets the stored Song rather than a stale copy.
	if updated, ok := provider.Current(); ok && updated.URL == current.URL {
		return updated, nil
	}

	current.File = download.Path
	current.Title = download.Title
	return current, nil
}

// Prefetch downloads the next ahead songs that have no cached file yet.
//
// It returns immediately; downloads run in the background so queueing stays
// responsive. Failures are logged rather than returned -- a Song that fails to
// prefetch is retried by EnsureCurrent when it is reached.
func (provider *MusicProvider) Prefetch(ctx context.Context, downloader *Downloader, ahead int) {
	for _, url := range provider.upcoming(ahead) {
		go func() {
			download, err := downloader.Get(ctx, url)
			if err != nil {
				slog.Warn("prefetching Song",
					slog.String("url", url),
					slog.Any("err", err),
				)
				return
			}
			provider.recordDownload(url, download)
		}()
	}
}

// upcoming returns the URLs of the next ahead songs after the current one that
// still need downloading.
func (provider *MusicProvider) upcoming(ahead int) []string {
	provider.lock.RLock()
	defer provider.lock.RUnlock()

	urls := make([]string, 0, ahead)
	for i := provider.queuePosition + 1; i < len(provider.songs) && len(urls) < ahead; i++ {
		if provider.songs[i].File == "" {
			urls = append(urls, provider.songs[i].URL)
		}
	}
	return urls
}

// recordDownload attaches a finished download to every queued Song with that
// URL. Matching on URL rather than index keeps this correct when the queue is
// reordered while a background download is in flight.
func (provider *MusicProvider) recordDownload(url string, download Download) {
	provider.lock.Lock()
	defer provider.lock.Unlock()

	for i := range provider.songs {
		if provider.songs[i].URL != url || provider.songs[i].File != "" {
			continue
		}
		provider.songs[i].File = download.Path
		provider.songs[i].Title = download.Title
	}
}
