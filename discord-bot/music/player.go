package music

import (
	"os"
	"os/exec"
	"sync"
)

type MusicPlayer interface {
	Queue(url string) error
	ClearQueue()
	Stop()
	PlayNext()
	PlayPrevious()
	CacheRetention() int
}

type Song struct {
	URL   string
	Title string
	File  string

	reader *FriendlyOpusReader
}

type MusicProvider struct {
	lock           sync.RWMutex
	songs          []Song
	playing        bool
	queuePosition  int
	maxQueueLength int
	cacheRetention int
}

type commandCloser struct {
	command *exec.Cmd
}

func (c *commandCloser) Close() error {
	_ = c.command.Process.Kill()
	return c.command.Wait()
}

func TranslateFile(path string) (*FriendlyOpusReader, error) {
	command := exec.Command("ffmpeg", "-i", path,
		"-f", "s16le",
		"-ar", "48000",
		"-ac", "2",
		"-loglevel", "error",
		"pipe:1",
	)

	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	command.Stderr = os.Stderr

	err = command.Start()

	if err != nil {
		return nil, err
	}

	return NewFriendlyOpusReader(stdout, &commandCloser{command: command})
}
