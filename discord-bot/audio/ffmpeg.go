package audio

import (
	"errors"
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
	return decode("-i", path, nil)
}

// DecodeReader is DecodeFile for audio that is already in hand rather than on
// disk, which is how a synthesised clip reaches the same PCM as a download.
// ffmpeg reads the container off stdin and works out what it is.
func DecodeReader(src io.Reader) (io.ReadCloser, error) {
	return decode("-i", "pipe:0", src)
}

func decode(input string, path string, stdin io.Reader) (io.ReadCloser, error) {
	command := exec.Command("ffmpeg", input, path,
		"-f", "s16le",
		"-ar", strconv.Itoa(SampleRate),
		"-ac", strconv.Itoa(Channels),
		"-loglevel", "error",
		"pipe:1",
	)

	command.Stdin = stdin

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

// StreamSpeech opens a clip as opus frames of its own, for when nothing is
// playing and the bot has the connection to itself. The Mixer comes back
// with the stream so a song that starts mid-line can go under it (SetBed),
// and the channel closes once the clip has been mixed in full.
func StreamSpeech(pcm io.ReadCloser) (*OpusStream, *Mixer, <-chan struct{}, error) {
	mixer := NewMixer(nil)

	// Queued before the stream starts: an empty Mixer ends immediately, which
	// would end the stream before a word came out.
	done, ok := mixer.Say(pcm)
	if !ok {
		return nil, nil, nil, errors.New("mixer refused the clip")
	}

	stream, err := NewOpusStream(mixer, mixer)
	if err != nil {
		_ = mixer.Close()
		return nil, nil, nil, err
	}
	return stream, mixer, done, nil
}
