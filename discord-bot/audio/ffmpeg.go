package audio

import (
	"io"
	"os"
	"os/exec"
	"strconv"
)

// process is an ffmpeg run whose stdout is the audio. Close kills it: the
// reader is done with the audio, and ffmpeg has no way to stop earlier.
type process struct {
	io.Reader
	command *exec.Cmd
}

func (p *process) Close() error {
	_ = p.command.Process.Kill()
	return p.command.Wait()
}

// DecodeFile spawns ffmpeg to turn path into s16le at Discord's rate and
// channel count. The reader is the process's stdout.
func DecodeFile(path string) (io.ReadCloser, error) {
	command := exec.Command("ffmpeg", "-i", path,
		"-f", "s16le",
		"-ar", strconv.Itoa(SampleRate),
		"-ac", strconv.Itoa(Channels),
		"-loglevel", "error",
		"pipe:1",
	)

	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	command.Stderr = os.Stderr

	if err := command.Start(); err != nil {
		return nil, err
	}

	return &process{Reader: stdout, command: command}, nil
}

// StreamFile opens path as opus frames ready for disgo.
func StreamFile(path string) (*OpusStream, error) {
	pcm, err := DecodeFile(path)
	if err != nil {
		return nil, err
	}
	return NewOpusStream(pcm, pcm)
}
