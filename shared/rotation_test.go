package shared

import (
	"sync"
	"testing"
	"time"
)

var testPolicy = RotationPolicy{Reward: 2 * time.Second, Punish: 5 * time.Second, Kill: 10 * time.Second, Parole: 5 * time.Minute}

func newTestRotation(items ...string) *genericRotation[string] {
	r, err := NewRotation(items, testPolicy)
	if err != nil {
		panic(err)
	}
	return r.(*genericRotation[string])
}

// compile-time check that genericRotation satisfies the interface
var _ Rotation[int] = (*genericRotation[int])(nil)

func TestNewRotationRejectsBadInput(t *testing.T) {
	if _, err := NewRotation([]string{}, testPolicy); err == nil {
		t.Error("empty items: want an error")
	}

	bad := map[string]RotationPolicy{
		"no parole":          {Reward: 1, Punish: 2, Kill: 3},
		"reward over punish": {Reward: 3, Punish: 2, Kill: 4, Parole: 1},
		"punish over kill":   {Reward: 1, Punish: 5, Kill: 4, Parole: 1},
	}
	for name, policy := range bad {
		if _, err := NewRotation([]string{"one"}, policy); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestNewRotationCopiesItems(t *testing.T) {
	items := []string{"one", "two"}
	r := newTestRotation(items...)
	items[0] = "changed"
	if got := r.Current().Value; got != "one" {
		t.Fatalf("Current = %s, want one", got)
	}
}

func TestFailRotatesOffTheDeadEntry(t *testing.T) {
	r := newTestRotation("one", "two", "three")

	r.Fail(r.Current())
	if got := r.Current().Value; got != "two" {
		t.Fatalf("after failing one, selected = %s, want two", got)
	}

	r.Fail(r.Current())
	if got := r.Current().Value; got != "three" {
		t.Fatalf("after failing two, selected = %s, want three", got)
	}

	// everything is benched now, so the stalest gets paroled instead of
	// hammering the entry just given up on
	r.Fail(r.Current())
	if got := r.Current().Value; got != "one" {
		t.Fatalf("all benched, selected = %s, want the stalest (one)", got)
	}
}

func TestJudgeSwapsOnceSlownessAddsUp(t *testing.T) {
	r := newTestRotation("one", "two")

	// a punished call costs 2, so it takes five before the swap
	for i := range 4 {
		if r.Judge(r.Current(), testPolicy.Punish) {
			t.Fatalf("benched after %d slow calls, too early", i+1)
		}
		if got := r.Current().Value; got != "one" {
			t.Fatalf("swapped after %d slow calls, too early", i+1)
		}
	}

	if !r.Judge(r.Current(), testPolicy.Punish) {
		t.Fatal("fifth slow call should report a bench")
	}
	if got := r.Current().Value; got != "two" {
		t.Fatalf("selected = %s, want two once one crossed the bench score", got)
	}
}

func TestJudgeBetweenRewardAndPunishIsNeutral(t *testing.T) {
	r := newTestRotation("one")
	r.Judge(r.Current(), testPolicy.Punish)
	r.Judge(r.Current(), (testPolicy.Reward+testPolicy.Punish)/2)
	if score := r.slots[0].score; score != 2 {
		t.Fatalf("score = %d, want 2 unchanged by a middling call", score)
	}
}

func TestJudgeKillsOutrightPastTheKillThreshold(t *testing.T) {
	r := newTestRotation("one", "two")
	if !r.Judge(r.Current(), testPolicy.Kill) {
		t.Fatal("kill latency should report a bench")
	}
	if got := r.Current().Value; got != "two" {
		t.Fatalf("selected = %s, want two", got)
	}
	if score := r.slots[0].score; score < benchScore {
		t.Errorf("killed score = %d, want it benched at %d", score, benchScore)
	}
}

func TestRewardIsFlooredAtZero(t *testing.T) {
	r := newTestRotation("one")
	for range 10 {
		r.Judge(r.Current(), testPolicy.Reward)
	}
	if score := r.slots[0].score; score != 0 {
		t.Fatalf("score = %d, want 0 so credit cannot be banked", score)
	}
}

func TestStaleVerdictDoesNotMoveTheRotation(t *testing.T) {
	r := newTestRotation("one", "two", "three")

	// a call that started on "one" lands after something else rotated to "two"
	stale := r.Current()
	r.Rotate()

	r.Judge(stale, testPolicy.Kill)
	if got := r.Current().Value; got != "two" {
		t.Fatalf("selected = %s, want two: a late verdict for one should not rotate again", got)
	}
	if score := r.slots[0].score; score < benchScore {
		t.Errorf("one's score = %d, want the verdict still counted against it", score)
	}
}

func TestParoleBringsABenchedEntryBack(t *testing.T) {
	r := newTestRotation("one", "two")

	r.Fail(r.Current())
	if got := r.Current().Value; got != "two" {
		t.Fatalf("selected = %s, want two", got)
	}

	// one has served its time; rotating off two should pick it back up
	r.slots[0].benched = time.Now().Add(-testPolicy.Parole - time.Second)
	r.Rotate()

	if got := r.Current().Value; got != "one" {
		t.Fatalf("selected = %s, want one back after parole", got)
	}
	if score := r.slots[0].score; score != 0 {
		t.Errorf("paroled score = %d, want a fresh 0", score)
	}
}

func TestRotationConcurrent(t *testing.T) {
	r := newTestRotation("one", "two", "three")

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for j := range 1000 {
				entry := r.Current()
				switch (i + j) % 4 {
				case 0:
					r.Fail(entry)
				case 1:
					r.Judge(entry, testPolicy.Punish)
				case 2:
					r.Judge(entry, testPolicy.Reward)
				case 3:
					r.Rotate()
				}
				_ = r.Len()
			}
		})
	}
	wg.Wait()
}
