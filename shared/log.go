package shared

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

const (
	latestLog   = "latest.log"
	archiveDir  = "archive"
	archiveTime = "2006-01-02_15-04-05"
)

type LogFile struct {
	dir  string
	file *os.File
}

func OpenLog(dir string) (*LogFile, error) {
	if err := os.MkdirAll(filepath.Join(dir, archiveDir), 0o755); err != nil {
		return nil, fmt.Errorf("making log directory: %w", err)
	}
	if err := archiveLatest(dir); err != nil {
		return nil, fmt.Errorf("archiving old latest.log: %w", err)
	}

	file, err := os.OpenFile(filepath.Join(dir, latestLog), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening latest.log: %w", err)
	}
	log.SetOutput(io.MultiWriter(os.Stderr, file))
	return &LogFile{dir: dir, file: file}, nil
}

func (l *LogFile) Close() error {
	log.SetOutput(os.Stderr)
	if err := l.file.Close(); err != nil {
		return fmt.Errorf("closing latest.log: %w", err)
	}
	return archiveLatest(l.dir)
}

func archiveLatest(dir string) error {
	latest := filepath.Join(dir, latestLog)
	info, err := os.Stat(latest)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	name := info.ModTime().Format(archiveTime) + ".log.gz"
	if err := gzipFile(latest, filepath.Join(dir, archiveDir, name)); err != nil {
		return err
	}
	return os.Remove(latest)
}

func gzipFile(from string, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(to)
	if err != nil {
		return err
	}
	defer out.Close()

	zip := gzip.NewWriter(out)
	if _, err := io.Copy(zip, in); err != nil {
		return err
	}

	if err := zip.Close(); err != nil {
		return err
	}
	return out.Close()
}
