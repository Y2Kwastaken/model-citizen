package shared

import (
	"fmt"
	"sync"
	"time"
)

// the score at which an entry is benched and skipped by the rotation
const benchScore = 10

// RotationPolicy is how a call's latency is judged and how long a benched
// entry sits out. A bench expires since an endpoint being down is nearly
// always temporary.
type RotationPolicy struct {
	// at or under this a call is rewarded, lowering the score by 1
	Reward time.Duration
	// at or over this a call is punished, raising the score by 2
	Punish time.Duration
	// at or over this a call benches the entry outright
	Kill time.Duration
	// how long a benched entry sits out before it is tried again
	Parole time.Duration
}

// Entry is a handle to one item in a rotation. Index is what lets a verdict
// that lands late still be counted against the right item.
type Entry[T any] struct {
	Value T
	Index int
}

type Rotation[T any] interface {
	// the entry requests should be sent to right now
	Current() Entry[T]
	// records how long a completed call took, swapping away from the entry
	// once it has been slow often enough to earn it. true if this verdict
	// benched the entry
	Judge(entry Entry[T], latency time.Duration) bool
	// benches an entry whose call errored outright and swaps off it
	Fail(entry Entry[T])
	// forces a swap to the next entry that is not benched
	Rotate()
	// the number of entries, which is how many failover attempts one
	// request is worth
	Len() int
}

// NewRotation creates a rotation over items, starting at the first. The items
// are copied, so later changes to the slice are not reflected.
func NewRotation[T any](items []T, policy RotationPolicy) (Rotation[T], error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("rotation needs at least one item")
	}
	if policy.Parole <= 0 {
		return nil, fmt.Errorf("rotation parole must be positive, got %s", policy.Parole)
	}
	if policy.Reward > policy.Punish || policy.Punish > policy.Kill {
		return nil, fmt.Errorf("rotation policy must have reward <= punish <= kill, got %s, %s, %s",
			policy.Reward, policy.Punish, policy.Kill)
	}

	slots := make([]slot[T], len(items))
	for i, item := range items {
		slots[i].value = item
	}
	return &genericRotation[T]{slots: slots, policy: policy}, nil
}

type slot[T any] struct {
	value T
	score int
	// when the slot was last benched
	benched time.Time
}

type genericRotation[T any] struct {
	lock     sync.RWMutex
	selected int
	slots    []slot[T]
	policy   RotationPolicy
}

func (r *genericRotation[T]) Current() Entry[T] {
	r.lock.RLock()
	defer r.lock.RUnlock()

	return Entry[T]{Value: r.slots[r.selected].value, Index: r.selected}
}

func (r *genericRotation[T]) Judge(entry Entry[T], latency time.Duration) bool {
	r.lock.Lock()
	defer r.lock.Unlock()

	judged := &r.slots[entry.Index]
	switch {
	case latency >= r.policy.Kill:
		judged.score = benchScore
	case latency <= r.policy.Reward:
		// floor so no good favor is built
		judged.score = max(judged.score-1, 0)
	case latency >= r.policy.Punish:
		judged.score += 2
	}

	if judged.score < benchScore {
		return false
	}
	judged.benched = time.Now()

	// a late verdict counts against the entry but doesn't rotate again
	if entry.Index == r.selected {
		r.next()
	}
	return true
}

func (r *genericRotation[T]) Fail(entry Entry[T]) {
	r.lock.Lock()
	defer r.lock.Unlock()

	failed := &r.slots[entry.Index]
	failed.score = benchScore
	failed.benched = time.Now()

	if entry.Index == r.selected {
		r.next()
	}
}

func (r *genericRotation[T]) Rotate() {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.next()
}

func (r *genericRotation[T]) Len() int {
	return len(r.slots)
}

// next moves selection to the next slot that is not benched, wrapping around.
// The caller must hold the write lock.
func (r *genericRotation[T]) next() {
	length := len(r.slots)
	now := time.Now()

	for i := 1; i <= length; i++ {
		index := (r.selected + i) % length
		candidate := &r.slots[index]
		if candidate.score >= benchScore {
			if now.Sub(candidate.benched) < r.policy.Parole {
				continue
			}
			candidate.score = 0 // paroled, it gets judged fresh from here
		}

		r.selected = index
		return
	}

	// nothing is healthy and nothing has served its parole, take longest benched
	stalest := 0
	for i := range r.slots {
		if r.slots[i].benched.Before(r.slots[stalest].benched) {
			stalest = i
		}
	}

	r.slots[stalest].score = 0
	r.selected = stalest
}
