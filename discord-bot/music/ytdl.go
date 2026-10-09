package music

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/Y2Kwastaken/model-citizen/shared"
	"github.com/lrstanley/go-ytdlp"
)

const linkPattern = `(https:\/\/)(www.|music.)?(youtube|youtu)(\.com|\.be)`

type ytdlService struct {
	downloader   *ytdlp.Command
	downloadPath string

	clock         sync.RWMutex
	cache         map[string]MusicDownload
	evictionQueue shared.CircularQueue[string]
	// inflight prevent multiple downloads require waiting instead
	lock        sync.Mutex
	downloading map[string]*sync.WaitGroup
}

func (s *ytdlService) Setup(ctx context.Context) error {
	if _, err := ytdlp.Install(ctx, nil); err != nil {
		return fmt.Errorf("installing yt-dlp: %w", err)
	}
	// ffmpeg/ffprobe come from the image (PATH); never download a second copy
	if _, err := ytdlp.InstallFFmpeg(ctx, &ytdlp.InstallFFmpegOptions{DisableDownload: true}); err != nil {
		return fmt.Errorf("finding ffmpeg: %w", err)
	}
	if _, err := ytdlp.InstallFFprobe(ctx, &ytdlp.InstallFFmpegOptions{DisableDownload: true}); err != nil {
		return fmt.Errorf("finding ffprobe: %w", err)
	}
	if _, err := ytdlp.InstallBun(ctx, nil); err != nil {
		return fmt.Errorf("installing bun: %w", err)
	}

	downloader := ytdlp.New().
		Format("bestaudio/best").
		NoPlaylist().
		NoOverwrites().
		Continue().
		Paths(s.downloadPath).
		Output("%(extractor)s - %(title)s.%(ext)s").
		PrintJSON()

	s.downloader = downloader
	return nil
}

func (s *ytdlService) Search(query string, ctx context.Context) (MusicDownload, error) {
	if s.downloader == nil {
		return MusicDownload{}, fmt.Errorf("youtube download isn't setup run Setup(ctx)")
	}

	formattedQuery, err := formatQuery(query)
	if err != nil {
		return MusicDownload{}, fmt.Errorf("formatting query while reading query %s: %w", query, err)
	}

	// hit cache aggressively for cheap repeated searches
	if dl, err := s.doSearchCache(formattedQuery); err == nil {
		return dl, nil
	}

	// we don't want to double download
	s.lock.Lock()

	// we must check again, a leading caller may have finished between our fast
	// path and this lock aquisition
	if dl, err := s.doSearchCache(formattedQuery); err == nil {
		s.lock.Unlock()
		return dl, nil
	}

	wg, ok := s.downloading[formattedQuery]
	if ok {
		// somebody else is downloading so we will do a cache lookup after it's done
		s.lock.Unlock()
		wg.Wait()
		return s.doSearchCache(formattedQuery)
	}

	// register before unlock so we don't get consumed by waiters
	wg = &sync.WaitGroup{}
	wg.Add(1)
	s.downloading[formattedQuery] = wg
	s.lock.Unlock()

	// defers and runs even if a panic occurs during our search
	defer func() {
		s.lock.Lock()
		delete(s.downloading, formattedQuery)
		s.lock.Unlock()
		wg.Done()
	}()

	return s.doSearchRequest(ctx, formattedQuery)
}

func (s *ytdlService) doSearchCache(query string) (MusicDownload, error) {
	s.clock.RLock()
	defer s.clock.RUnlock()
	download, ok := s.cache[query]
	if !ok {
		return MusicDownload{}, fmt.Errorf("cache miss for formatted query %s download likely failed", query)
	}

	return download, nil
}

func (s *ytdlService) doSearchRequest(ctx context.Context, query string) (MusicDownload, error) {
	result, err := s.downloader.Run(ctx, query)
	if err != nil {
		return MusicDownload{}, fmt.Errorf("downloading from formatted query %s: %w", query, err)
	}

	download, err := extractInfo(result, query)
	if err != nil {
		return MusicDownload{}, err
	}

	s.clock.Lock()
	defer s.clock.Unlock()

	remove, ok := s.evictionQueue.Enqueue(query)
	if ok {
		removedDownload := s.cache[remove]
		delete(s.cache, remove)

		err := os.Remove(removedDownload.Path)
		if err != nil {
			return MusicDownload{}, err
		}

	}

	s.cache[query] = download
	return download, nil
}

func extractInfo(result *ytdlp.Result, query string) (MusicDownload, error) {
	info, err := result.GetExtractedInfo()
	if err != nil {
		return MusicDownload{}, fmt.Errorf("extracting video infomration for formatted query %s: %w", query, err)
	}

	if len(info) == 0 {
		return MusicDownload{}, fmt.Errorf("no video information for formatted query %s: %w", query, err)
	}

	first := info[0]

	var duration time.Duration
	if first.Duration != nil {
		duration = time.Duration(*first.Duration * float64(time.Second))
	}

	return MusicDownload{
		Id:       first.ID,
		Label:    *first.Title,
		Duration: duration,
		Path:     *first.Filename,
	}, nil
}

func formatQuery(query string) (string, error) {
	match, err := regexp.MatchString(linkPattern, query)
	if err != nil {
		return "", err
	}

	// probably not a url
	if !match {
		query = "ytsearch1:" + query
	}

	return query, nil
}
