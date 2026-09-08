package music

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/lrstanley/go-ytdlp"
)

// CacheDir is where downloaded audio lives. It matches the music-provider
// volume mounted by docker-compose, so the cache survives restarts.
const CacheDir = "/music"

// EnsureYtdlp resolves the yt-dlp binary, downloading it if needed.
//
// This installs the exact yt-dlp release that the go-ytdlp module was built
// against -- not whatever is newest. Bumping the go-ytdlp dependency is what
// moves yt-dlp forward, and the version check re-downloads when it changes.
// That matters more than it sounds: sites break yt-dlp extractors regularly,
// so a stale binary fails at runtime rather than at build time.
//
// ffmpeg is deliberately not installed here. go-ytdlp resolves PATH before
// downloading, so it finds the pinned build already in the image.
func EnsureYtdlp(ctx context.Context) error {
	resolved, err := ytdlp.Install(ctx, nil)
	if err != nil {
		return fmt.Errorf("installing yt-dlp: %w", err)
	}

	slog.Info("yt-dlp ready",
		slog.String("path", resolved.Executable),
		slog.String("version", resolved.Version),
		slog.Bool("downloaded", resolved.Downloaded),
	)
	return nil
}

// Download is a cached audio file plus the metadata that came with it.
type Download struct {
	ID    string
	Title string
	Path  string
}

// Downloader fetches audio into a cache directory, one file per video ID.
type Downloader struct {
	dir       string
	retention int

	// inflight collapses concurrent requests for the same URL into one
	// download -- without it, a prefetch and a play of the same song race to
	// write the same file.
	mu       sync.Mutex
	inflight map[string]chan struct{}
}

// NewDownloader returns a Downloader writing into dir. A retention of zero or
// less keeps every file.
func NewDownloader(dir string, retention int) (*Downloader, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating cache dir %s: %w", dir, err)
	}

	return &Downloader{
		dir:       dir,
		retention: retention,
		inflight:  make(map[string]chan struct{}),
	}, nil
}

// Get returns the cached file for url, downloading it if it is not present.
// It blocks until the file is ready.
func (downloader *Downloader) Get(ctx context.Context, url string) (Download, error) {
	// Wait out any download of this URL already in progress, then retry: the
	// file it produces is the one we want.
	downloader.mu.Lock()
	if wait, ok := downloader.inflight[url]; ok {
		downloader.mu.Unlock()
		select {
		case <-wait:
			return downloader.Get(ctx, url)
		case <-ctx.Done():
			return Download{}, ctx.Err()
		}
	}
	done := make(chan struct{})
	downloader.inflight[url] = done
	downloader.mu.Unlock()

	defer func() {
		downloader.mu.Lock()
		delete(downloader.inflight, url)
		downloader.mu.Unlock()
		close(done)
	}()

	// yt-dlp skips the download itself when the output file already exists, so
	// a cache hit costs a metadata lookup rather than a transfer.
	result, err := ytdlp.New().
		Format("bestaudio/best").
		Output(filepath.Join(downloader.dir, "%(id)s.%(ext)s")).
		NoPlaylist().
		NoProgress().
		PrintJSON().
		Run(ctx, url)
	if err != nil {
		return Download{}, fmt.Errorf("downloading %s: %w", url, err)
	}

	infos, err := result.GetExtractedInfo()
	if err != nil {
		return Download{}, fmt.Errorf("reading yt-dlp output for %s: %w", url, err)
	}
	if len(infos) == 0 {
		return Download{}, fmt.Errorf("yt-dlp returned no media for %s", url)
	}
	info := infos[0]

	path, err := downloader.locate(info)
	if err != nil {
		return Download{}, err
	}

	download := Download{ID: info.ID, Path: path}
	if info.Title != nil {
		download.Title = *info.Title
	}

	if err := downloader.evict(path); err != nil {
		// A full cache is not a reason to refuse to play what we just fetched.
		slog.Warn("evicting cached audio", slog.Any("err", err))
	}

	return download, nil
}

// locate finds the file yt-dlp produced. It reports the path itself, but falls
// back to matching the video ID when the field is absent.
func (downloader *Downloader) locate(info *ytdlp.ExtractedInfo) (string, error) {
	if info.Filename != nil && *info.Filename != "" {
		if _, err := os.Stat(*info.Filename); err == nil {
			return *info.Filename, nil
		}
	}

	matches, err := filepath.Glob(filepath.Join(downloader.dir, info.ID+".*"))
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("no cached file for %s", info.ID)
	}
	return matches[0], nil
}

// evict trims the cache to the retention limit, oldest first, never removing
// keep.
//
// Deleting a file that ffmpeg currently has open is safe on Linux: the process
// holds the inode until it closes, so playback of an evicted track continues.
func (downloader *Downloader) evict(keep string) error {
	if downloader.retention <= 0 {
		return nil
	}

	entries, err := os.ReadDir(downloader.dir)
	if err != nil {
		return err
	}

	type cached struct {
		path    string
		modTime int64
	}

	files := make([]cached, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(downloader.dir, entry.Name())
		if path == keep {
			continue
		}
		files = append(files, cached{path: path, modTime: info.ModTime().UnixNano()})
	}

	// Newest first, so anything past the limit is the oldest.
	slices.SortFunc(files, func(a, b cached) int {
		return cmp.Compare(b.modTime, a.modTime)
	})

	// keep occupies one slot of the retention budget.
	limit := downloader.retention - 1
	if limit < 0 {
		limit = 0
	}
	if len(files) <= limit {
		return nil
	}

	for _, file := range files[limit:] {
		if err := os.Remove(file.path); err != nil {
			return err
		}
		slog.Debug("evicted cached audio", slog.String("path", file.path))
	}
	return nil
}
