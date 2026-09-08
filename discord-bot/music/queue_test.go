package music

import (
	"fmt"
	"sync"
	"testing"
)

func TestQueueConcurrent(t *testing.T) {
	p := NewMusicProvider(0, 5)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = p.Queue(fmt.Sprintf("url-%d-%d", n, j))
				p.QueueLength()
				p.Current()
				p.Advance()
				p.Rewind()
				p.Songs()
				p.Playing()
				p.SetPlaying(j%2 == 0)
				p.Position()
				p.CacheRetention()
				if j%50 == 0 {
					_ = p.Remove(0)
				}
			}
		}(i)
	}
	wg.Wait()
	t.Logf("final length=%d position=%d", p.QueueLength(), p.Position())
}

func TestQueueSemantics(t *testing.T) {
	p := NewMusicProvider(2, 3)

	if err := p.Queue("a"); err != nil {
		t.Fatal(err)
	}
	if err := p.Queue("b"); err != nil {
		t.Fatal(err)
	}
	if err := p.Queue("c"); err == nil {
		t.Fatal("expected queue-full error at maxQueueLength=2")
	}

	if s, ok := p.Current(); !ok || s.URL != "a" {
		t.Fatalf("current = %v %v, want a true", s.URL, ok)
	}
	if s, ok := p.Advance(); !ok || s.URL != "b" {
		t.Fatalf("advance = %v %v, want b true", s.URL, ok)
	}
	if _, ok := p.Advance(); ok {
		t.Fatal("advance past the end should report false")
	}
	if p.Position() != 1 {
		t.Fatalf("position = %d, want 1 (unchanged by failed advance)", p.Position())
	}
	if s, ok := p.Rewind(); !ok || s.URL != "a" {
		t.Fatalf("rewind = %v %v, want a true", s.URL, ok)
	}
	if _, ok := p.Rewind(); ok {
		t.Fatal("rewind before the start should report false")
	}

	p.ClearQueue()
	if p.QueueLength() != 0 || p.Position() != 0 {
		t.Fatal("ClearQueue should empty the queue and reset position")
	}
	if _, ok := p.Current(); ok {
		t.Fatal("Current on an empty queue should report false")
	}
}
