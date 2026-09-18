package discordbot

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/godave"
)

// silenceTail is the longest run of undecryptable packets that is still
// just the end of a talk spurt. Discord clients send five silence frames
// there, which DAVE does not encrypt and disgo tries to decrypt anyway; a
// couple of stragglers can join the burst. Longer runs are speech being
// lost, and that is worth a warning.
const silenceTail = 8

// daveFailureSummaryEvery is how often a tally that includes real loss is
// reported.
const daveFailureSummaryEvery = time.Minute

// quietUDP wraps disgo's UDP connection so the silence tails are dropped
// where they arrive instead of surfacing as an ERROR line each. The tally
// keeps the count and burst sizes, and only speaks up when a burst is too
// long to be a silence tail.
type quietUDP struct {
	voice.UDPConn
	failures *daveFailures
}

func newQuietUDP(session godave.Session, lookup voice.SsrcLookupFunc, opts ...voice.UDPConnConfigOpt) voice.UDPConn {
	return &quietUDP{UDPConn: voice.NewUDPConn(session, lookup, opts...), failures: &daveFailures{lastFlush: time.Now()}}
}

func (u *quietUDP) ReadPacket() (*voice.Packet, error) {
	for {
		packet, err := u.UDPConn.ReadPacket()
		if err != nil && strings.Contains(err.Error(), "failed to DAVE decrypt") {
			u.failures.record(time.Now())
			continue
		}
		return packet, err
	}
}

// daveFailures is the running tally of dropped packets.
type daveFailures struct {
	mu        sync.Mutex
	count     int
	bursts    int
	maxBurst  int
	burst     int
	lastFail  time.Time
	lastFlush time.Time
}

func (f *daveFailures) record(at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.count++
	// failures closer together than a few packet intervals are one burst
	if at.Sub(f.lastFail) > 100*time.Millisecond {
		f.bursts++
		f.burst = 0
	}
	f.burst++
	f.maxBurst = max(f.maxBurst, f.burst)
	f.lastFail = at

	if at.Sub(f.lastFlush) < daveFailureSummaryEvery {
		return
	}
	if f.maxBurst > silenceTail {
		slog.Warn("voice packets failed to decrypt beyond the silence tails",
			slog.Int("packets", f.count),
			slog.Int("bursts", f.bursts),
			slog.Int("max_burst", f.maxBurst),
			slog.Duration("over", at.Sub(f.lastFlush).Round(time.Second)),
		)
	}
	f.count, f.bursts, f.maxBurst, f.lastFlush = 0, 0, 0, at
}
