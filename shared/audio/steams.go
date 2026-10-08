package audio

import (
	"io"
	"os"
	"os/exec"
)

type AudioPipe struct {
	Reader io.ReadCloser
	src    *exec.Cmd
}

func (ap *AudioPipe) Wait() {
	ap.src.Wait()
}

func (p AudioPipe) Close() error {
	p.src.Process.Kill() // no op if exited
	return p.src.Wait()
}

func WavToPcmStream(filePath string) (AudioPipe, error) {
	cmd := exec.Command(
		"ffmpeg", "-i", filePath,
		"-hide_banner",
		"-loglevel", "error",
		"-nostats",
		"-nostdin",
		"-f", "s16le",
		"-ar", "48000",
		"-ac", "2",
		"pipe:1",
	)
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return AudioPipe{}, err
	}

	if err := cmd.Start(); err != nil {
		return AudioPipe{}, err
	}

	return AudioPipe{
		Reader: stdout,
		src:    cmd,
	}, nil
}
