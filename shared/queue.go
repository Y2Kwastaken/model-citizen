package shared

import (
	"fmt"
	"iter"
	"slices"
	"sync"
)

type CircularQueue[T any] interface {
	// enqueues a value into the queue, returns the evicted value and true
	// if the queue was full and the oldest value was evicted, otherwise the zero value and false
	Enqueue(val T) (T, bool)
	// dequeues an element from the queue, true if an item was
	// dequeued otherwise false. false returns the zero value
	Dequeue() (T, bool)
	// the amount of elements in the queue
	Size() int
	// the maximum queue capacity
	Capacity() int
	// a copy of the queued elements, oldest first
	Items() []T
	// iterates over a snapshot of the queued elements, oldest first. the
	// queue may be modified while iterating; changes are not reflected
	All() iter.Seq[T]
	// removes every element, the capacity is unchanged
	Clear()
}

// NewCircularQueue creates a queue holding at most capacity elements. Once
// full, each Enqueue evicts the oldest element.
func NewCircularQueue[T any](capacity int) (CircularQueue[T], error) {
	if capacity <= 0 {
		return nil, fmt.Errorf("circular queue capacity must be positive, got %d", capacity)
	}
	return &genericQueue[T]{
		queue:    make([]T, capacity),
		capacity: capacity,
	}, nil
}

type genericQueue[T any] struct {
	lock     sync.Mutex
	queue    []T
	head     int // next slot to write
	tail     int // next slot to read (oldest element)
	size     int
	capacity int
}

func (q *genericQueue[T]) Enqueue(val T) (T, bool) {
	q.lock.Lock()
	defer q.lock.Unlock()

	var lastVal T
	looped := q.isFull()
	if looped {
		// when full, head has caught up to tail, so the slot we're about
		// to overwrite holds the oldest element
		lastVal = q.queue[q.tail]
		q.tail = q.nextTail()
	} else {
		q.size += 1
	}
	q.queue[q.head] = val
	q.head = q.nextHead()
	return lastVal, looped
}

func (q *genericQueue[T]) Dequeue() (T, bool) {
	q.lock.Lock()
	defer q.lock.Unlock()

	var item T
	if q.isEmpty() {
		return item, false
	}

	item = q.queue[q.tail]
	q.tail = q.nextTail()
	q.size -= 1
	return item, true
}

func (q *genericQueue[T]) Size() int {
	q.lock.Lock()
	defer q.lock.Unlock()

	return q.size
}

func (q *genericQueue[T]) Capacity() int {
	return q.capacity
}

func (q *genericQueue[T]) Items() []T {
	q.lock.Lock()
	defer q.lock.Unlock()

	out := make([]T, 0, q.size)
	for i := range q.size {
		out = append(out, q.queue[(q.tail+i)%q.capacity])
	}
	return out
}

func (q *genericQueue[T]) All() iter.Seq[T] {
	return slices.Values(q.Items())
}

func (q *genericQueue[T]) Clear() {
	q.lock.Lock()
	defer q.lock.Unlock()

	// zero the slots so the queue doesn't keep old elements alive for the GC
	clear(q.queue)
	q.head = 0
	q.tail = 0
	q.size = 0
}

func (q *genericQueue[T]) nextHead() int {
	return (q.head + 1) % q.capacity
}

func (q *genericQueue[T]) nextTail() int {
	return (q.tail + 1) % q.capacity
}

func (q *genericQueue[T]) isEmpty() bool {
	return q.size == 0
}

func (q *genericQueue[T]) isFull() bool {
	return q.size == q.capacity
}
