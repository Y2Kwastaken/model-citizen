package audio

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"
)

// silentPCM returns n frames worth of s16le silence.
func silentPCM(frames int) io.Reader {
	return bytes.NewReader(make([]byte, FrameBytes*frames))
}

// TestDoneFiresOnlyAfterDrain is the load-bearing guarantee of the buffered
// design: the producer finishes well ahead of the consumer, so signalling
// completion when the producer exits would cut off every buffered frame.
func TestDoneFiresOnlyAfterDrain(t *testing.T) {
	const frames = 7

	reader, err := NewOpusStream(silentPCM(frames), nil)
	if err != nil {
		t.Fatal(err)
	}

	// Give the producer time to finish everything and close its channel.
	time.Sleep(100 * time.Millisecond)

	for i := range frames {
		select {
		case <-reader.Done():
			t.Fatalf("Done fired with %d frames still unplayed", frames-i)
		default:
		}

		frame, err := reader.ProvideOpusFrame()
		if err != nil {
			t.Fatalf("frame %d: unexpected error %v", i, err)
		}
		if len(frame) == 0 {
			t.Fatalf("frame %d: empty", i)
		}
	}

	if _, err := reader.ProvideOpusFrame(); !errors.Is(err, io.EOF) {
		t.Fatalf("after the last frame: got %v, want io.EOF", err)
	}

	select {
	case <-reader.Done():
	case <-time.After(time.Second):
		t.Fatal("Done never fired after the stream drained")
	}
}

// TestProvideAfterEndStaysQuiet covers disgo polling every 20ms forever after a
// track ends: those polls must report a clean EOF, not a closed-pipe error.
func TestProvideAfterEndStaysQuiet(t *testing.T) {
	reader, err := NewOpusStream(silentPCM(2), nil)
	if err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if _, err := reader.ProvideOpusFrame(); err != nil {
			t.Fatalf("unexpected error draining: %v", err)
		}
	}

	for i := range 5 {
		if _, err := reader.ProvideOpusFrame(); !errors.Is(err, io.EOF) {
			t.Fatalf("poll %d after end: got %v, want io.EOF", i, err)
		}
	}
}

// TestCloseUnblocksProducer checks that Close does not deadlock against a
// producer parked on a full buffer.
func TestCloseUnblocksProducer(t *testing.T) {
	// Far more frames than the buffer holds, so the producer parks on send.
	reader, err := NewOpusStream(silentPCM(bufferedFrames*2), nil)
	if err != nil {
		t.Fatal(err)
	}

	closed := make(chan struct{})
	go func() {
		reader.Close()
		close(closed)
	}()

	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close deadlocked against a parked producer")
	}

	// Draining after a close must terminate rather than hang.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("draining after Close did not terminate")
		default:
		}
		if _, err := reader.ProvideOpusFrame(); err != nil {
			return
		}
	}
}

// TestPrefillDoesNotStallShortTracks guards the constructor: a track shorter
// than prefillFrames must not wait out prefillTimeout.
func TestPrefillDoesNotStallShortTracks(t *testing.T) {
	start := time.Now()
	reader, err := NewOpusStream(silentPCM(3), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("constructor blocked %v on a %d-frame track", elapsed, 3)
	}
}
