package events

import "testing"

func TestWakeWordStrip(t *testing.T) {
	model := newWakeWord([]string{"model"})
	kay := newWakeWord([]string{"kay", "k", "kaye", "cay"})

	tests := []struct {
		word wakeWord
		in   string
		want string
	}{
		{model, "Hey model, play some music.", "play some music."},
		{model, "A model what time is it", "what time is it"},
		{model, "this model is bad", "this model is bad"},
		{kay, "Hey Kay, play some music.", "play some music."},
		{kay, "Hey, K. What's two plus two?", "What's two plus two?"},
		{kay, "Kaye, skip this", "skip this"},
		{kay, "so I told him hey kay what's up", "so I told him what's up"},
		{kay, "okay play something", "okay play something"},
		{kay, "the kayak flipped", "the kayak flipped"},
	}
	for _, test := range tests {
		if got := test.word.strip(test.in); got != test.want {
			t.Errorf("strip(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}
