package discordbot

import (
	"testing"
	"time"

	"github.com/disgoorg/snowflake/v2"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/audio"
)

func utterance(user int, start time.Time, length time.Duration) audio.Utterance {
	return audio.Utterance{
		User:  snowflake.ID(user),
		Start: start,
		PCM:   make([]int16, int(length*audio.SampleRate/time.Second)),
	}
}

func TestClosedBeforeLeavesOpenUtterances(t *testing.T) {
	trigger := time.Date(2026, 9, 16, 12, 0, 10, 0, time.UTC)
	utts := []audio.Utterance{
		utterance(1, trigger.Add(-8*time.Second), 2*time.Second),                 // ended 6s ago
		utterance(2, trigger.Add(-3*time.Second), 2*time.Second),                 // ended 1s ago
		utterance(1, trigger.Add(-1500*time.Millisecond), 1400*time.Millisecond), // "hey model", still going
	}

	closed := closedBefore(utts, trigger)
	if len(closed) != 2 || closed[0].User != 1 || closed[1].User != 2 {
		t.Fatalf("closed = %+v, want the first two", closed)
	}
}

func TestExcludingDropsWhatWasAlreadySent(t *testing.T) {
	base := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	early := []audio.Utterance{utterance(1, base, time.Second), utterance(2, base.Add(2*time.Second), time.Second)}
	all := append(append([]audio.Utterance{}, early...), utterance(1, base.Add(5*time.Second), 3*time.Second))

	late := excluding(all, early)
	if len(late) != 1 || !late[0].Start.Equal(base.Add(5*time.Second)) {
		t.Fatalf("late = %+v, want only the new utterance", late)
	}
}
