package discordbot

import (
	"errors"
	"testing"
	"time"

	"github.com/disgoorg/disgo/voice"
)

// scriptedUDP hands back a fixed sequence of ReadPacket results.
type scriptedUDP struct {
	voice.UDPConn
	results []error
}

func (s *scriptedUDP) ReadPacket() (*voice.Packet, error) {
	err := s.results[0]
	s.results = s.results[1:]
	if err != nil {
		return nil, err
	}
	return &voice.Packet{}, nil
}

var daveErr = errors.New("failed to DAVE decrypt packet: failed to decrypt frame")

func TestQuietUDPDropsSilenceTailsAndKeepsThePacket(t *testing.T) {
	inner := &scriptedUDP{results: []error{daveErr, daveErr, daveErr, daveErr, daveErr, nil}}
	u := &quietUDP{UDPConn: inner, failures: &daveFailures{lastFlush: time.Now()}}

	packet, err := u.ReadPacket()
	if err != nil || packet == nil {
		t.Fatalf("got %v, %v; want the packet after the silence tail", packet, err)
	}
	if u.failures.count != 5 || u.failures.bursts != 1 || u.failures.maxBurst != 5 {
		t.Fatalf("tally = %d packets, %d bursts, max %d; want one burst of five", u.failures.count, u.failures.bursts, u.failures.maxBurst)
	}
}

func TestQuietUDPPassesOtherErrors(t *testing.T) {
	want := errors.New("failed to read packet: connection reset")
	u := &quietUDP{UDPConn: &scriptedUDP{results: []error{want}}, failures: &daveFailures{lastFlush: time.Now()}}
	if _, err := u.ReadPacket(); !errors.Is(err, want) {
		t.Fatalf("got %v, want the read error through untouched", err)
	}
}

func TestDaveFailuresCountsBursts(t *testing.T) {
	f := &daveFailures{lastFlush: time.Now()}
	at := time.Now()
	for range 3 { // three talk spurts
		at = at.Add(2 * time.Second)
		for i := range 5 {
			f.record(at.Add(time.Duration(i) * 20 * time.Millisecond))
		}
	}
	if f.count != 15 || f.bursts != 3 || f.maxBurst != 5 {
		t.Fatalf("tally = %d packets, %d bursts, max %d; want 15 in 3 bursts of 5", f.count, f.bursts, f.maxBurst)
	}
}
