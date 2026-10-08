package shared

import (
	"math/rand"
	"slices"
	"sync"
	"testing"
)

func newTestQueue[T any](capacity int) CircularQueue[T] {
	q, err := NewCircularQueue[T](capacity)
	if err != nil {
		panic(err)
	}
	return q
}

func TestNewCircularQueueRejectsNonPositiveCapacity(t *testing.T) {
	for _, capacity := range []int{0, -1} {
		if _, err := NewCircularQueue[int](capacity); err == nil {
			t.Fatalf("NewCircularQueue(%d) returned nil error", capacity)
		}
	}
}

// compile-time check that genericQueue satisfies the interface
var _ CircularQueue[int] = (*genericQueue[int])(nil)

func TestDequeueEmpty(t *testing.T) {
	q := newTestQueue[int](4)

	val, ok := q.Dequeue()
	if ok {
		t.Fatalf("Dequeue on empty queue returned ok=true (val=%d)", val)
	}
	if val != 0 {
		t.Fatalf("Dequeue on empty queue returned %d, want zero value", val)
	}
	if q.Size() != 0 {
		t.Fatalf("Size = %d, want 0", q.Size())
	}
}

func TestCapacity(t *testing.T) {
	q := newTestQueue[int](7)
	if q.Capacity() != 7 {
		t.Fatalf("Capacity = %d, want 7", q.Capacity())
	}
}

func TestFIFOOrder(t *testing.T) {
	q := newTestQueue[int](4)

	for i := 1; i <= 3; i++ {
		if evicted, looped := q.Enqueue(i); looped {
			t.Fatalf("Enqueue(%d) evicted %d on a non-full queue", i, evicted)
		}
	}
	if q.Size() != 3 {
		t.Fatalf("Size = %d, want 3", q.Size())
	}

	for want := 1; want <= 3; want++ {
		got, ok := q.Dequeue()
		if !ok || got != want {
			t.Fatalf("Dequeue = (%d, %v), want (%d, true)", got, ok, want)
		}
	}
	if _, ok := q.Dequeue(); ok {
		t.Fatal("Dequeue after draining returned ok=true")
	}
}

func TestFillToCapacityDoesNotEvict(t *testing.T) {
	q := newTestQueue[int](4)

	for i := 1; i <= 4; i++ {
		if evicted, looped := q.Enqueue(i); looped {
			t.Fatalf("Enqueue(%d) evicted %d before queue was over capacity", i, evicted)
		}
	}
	if q.Size() != 4 {
		t.Fatalf("Size = %d, want 4", q.Size())
	}
	for want := 1; want <= 4; want++ {
		got, ok := q.Dequeue()
		if !ok || got != want {
			t.Fatalf("Dequeue = (%d, %v), want (%d, true)", got, ok, want)
		}
	}
}

func TestEnqueueFullEvictsOldest(t *testing.T) {
	q := newTestQueue[int](3)
	for i := 1; i <= 3; i++ {
		q.Enqueue(i)
	}

	// queue: [1 2 3]; pushing 4 should evict 1
	evicted, looped := q.Enqueue(4)
	if !looped || evicted != 1 {
		t.Fatalf("Enqueue(4) on full queue = (%d, %v), want (1, true)", evicted, looped)
	}
	if q.Size() != 3 {
		t.Fatalf("Size after overwrite = %d, want 3", q.Size())
	}

	for want := 2; want <= 4; want++ {
		got, ok := q.Dequeue()
		if !ok || got != want {
			t.Fatalf("Dequeue = (%d, %v), want (%d, true)", got, ok, want)
		}
	}
}

func TestEnqueueFullManyWraps(t *testing.T) {
	const capacity = 3
	q := newTestQueue[int](capacity)

	// push far more than capacity; every push past the first `capacity`
	// should evict exactly the value pushed `capacity` steps earlier
	for i := 0; i < 10*capacity; i++ {
		evicted, looped := q.Enqueue(i)
		if i < capacity {
			if looped {
				t.Fatalf("Enqueue(%d) evicted %d before queue was full", i, evicted)
			}
			continue
		}
		if !looped || evicted != i-capacity {
			t.Fatalf("Enqueue(%d) = (%d, %v), want (%d, true)", i, evicted, looped, i-capacity)
		}
		if q.Size() != capacity {
			t.Fatalf("Size after Enqueue(%d) = %d, want %d", i, q.Size(), capacity)
		}
	}
}

func TestCapacityOne(t *testing.T) {
	q := newTestQueue[string](1)

	if _, looped := q.Enqueue("a"); looped {
		t.Fatal("first Enqueue evicted on capacity-1 queue")
	}
	evicted, looped := q.Enqueue("b")
	if !looped || evicted != "a" {
		t.Fatalf("Enqueue(b) = (%q, %v), want (\"a\", true)", evicted, looped)
	}
	got, ok := q.Dequeue()
	if !ok || got != "b" {
		t.Fatalf("Dequeue = (%q, %v), want (\"b\", true)", got, ok)
	}
	if q.Size() != 0 {
		t.Fatalf("Size = %d, want 0", q.Size())
	}
}

func TestInterleavedAcrossWrap(t *testing.T) {
	q := newTestQueue[int](3)

	// move head/tail around the ring a few times without ever filling it
	next, want := 0, 0
	for round := 0; round < 5; round++ {
		q.Enqueue(next)
		next++
		q.Enqueue(next)
		next++

		for range 2 {
			got, ok := q.Dequeue()
			if !ok || got != want {
				t.Fatalf("round %d: Dequeue = (%d, %v), want (%d, true)", round, got, ok, want)
			}
			want++
		}
	}
}

func TestItemsEmpty(t *testing.T) {
	q := newTestQueue[int](3)
	if got := q.Items(); len(got) != 0 {
		t.Fatalf("Items on empty queue = %v, want empty", got)
	}
}

func TestItemsIsCopy(t *testing.T) {
	q := newTestQueue[int](3)
	q.Enqueue(1)
	q.Enqueue(2)

	items := q.Items()
	items[0] = 99
	if got, _ := q.Dequeue(); got != 1 {
		t.Fatalf("mutating Items result changed queue: Dequeue = %d, want 1", got)
	}
}

func TestClear(t *testing.T) {
	q := newTestQueue[int](3)
	// wrap first so Clear has to reset a non-zero head and tail
	for i := 1; i <= 5; i++ {
		q.Enqueue(i)
	}

	q.Clear()
	if q.Size() != 0 {
		t.Fatalf("Size after Clear = %d, want 0", q.Size())
	}
	if items := q.Items(); len(items) != 0 {
		t.Fatalf("Items after Clear = %v, want empty", items)
	}
	if val, ok := q.Dequeue(); ok {
		t.Fatalf("Dequeue after Clear returned %d, ok=true", val)
	}

	// the queue is still usable at full capacity
	for i := 10; i <= 12; i++ {
		if evicted, looped := q.Enqueue(i); looped {
			t.Fatalf("Enqueue(%d) after Clear evicted %d", i, evicted)
		}
	}
	if got, want := q.Items(), []int{10, 11, 12}; !slices.Equal(got, want) {
		t.Fatalf("Items = %v, want %v", got, want)
	}
}

func TestAllAcrossWrap(t *testing.T) {
	q := newTestQueue[int](3)
	for i := 1; i <= 5; i++ { // wraps; queue holds [3 4 5]
		q.Enqueue(i)
	}

	got := slices.Collect(q.All())
	if want := []int{3, 4, 5}; !slices.Equal(got, want) {
		t.Fatalf("All = %v, want %v", got, want)
	}
}

func TestAllAllowsModificationWhileIterating(t *testing.T) {
	q := newTestQueue[int](3)
	q.Enqueue(1)
	q.Enqueue(2)

	// would deadlock if All held the lock while yielding
	var got []int
	for v := range q.All() {
		got = append(got, v)
		q.Dequeue()
		q.Enqueue(v * 10)
	}
	if want := []int{1, 2}; !slices.Equal(got, want) {
		t.Fatalf("All = %v, want %v", got, want)
	}
	if want := []int{10, 20}; !slices.Equal(q.Items(), want) {
		t.Fatalf("Items after loop = %v, want %v", q.Items(), want)
	}
}

// TestAgainstModel runs random operations against both the queue and a plain
// slice that implements the same semantics, and checks they always agree.
func TestAgainstModel(t *testing.T) {
	for _, capacity := range []int{1, 2, 3, 5, 8} {
		rng := rand.New(rand.NewSource(int64(capacity)))
		q := newTestQueue[int](capacity)
		var model []int

		for step := 0; step < 2000; step++ {
			if rng.Intn(3) != 0 { // enqueue ~2/3 of the time so it fills up
				val := step
				gotEvicted, gotLooped := q.Enqueue(val)

				var wantEvicted int
				wantLooped := len(model) == capacity
				if wantLooped {
					wantEvicted, model = model[0], model[1:]
				}
				model = append(model, val)

				if gotLooped != wantLooped || gotEvicted != wantEvicted {
					t.Fatalf("cap=%d step=%d: Enqueue(%d) = (%d, %v), want (%d, %v)",
						capacity, step, val, gotEvicted, gotLooped, wantEvicted, wantLooped)
				}
			} else {
				got, ok := q.Dequeue()

				var want int
				wantOK := len(model) > 0
				if wantOK {
					want, model = model[0], model[1:]
				}

				if ok != wantOK || got != want {
					t.Fatalf("cap=%d step=%d: Dequeue = (%d, %v), want (%d, %v)",
						capacity, step, got, ok, want, wantOK)
				}
			}

			if q.Size() != len(model) {
				t.Fatalf("cap=%d step=%d: Size = %d, want %d", capacity, step, q.Size(), len(model))
			}
			if got := q.Items(); !slices.Equal(got, model) {
				t.Fatalf("cap=%d step=%d: Items = %v, want %v", capacity, step, got, model)
			}
		}
	}
}

// TestConcurrent is mainly useful under `go test -race`.
func TestConcurrent(t *testing.T) {
	const (
		producers   = 8
		perProducer = 1000
		total       = producers * perProducer
	)
	// big enough that nothing is ever evicted
	q := newTestQueue[int](total)

	var wg sync.WaitGroup
	for p := range producers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range perProducer {
				q.Enqueue(p*perProducer + i)
				_ = q.Size()
			}
		}()
	}
	wg.Wait()

	if q.Size() != total {
		t.Fatalf("Size = %d, want %d", q.Size(), total)
	}

	seen := make([]bool, total)
	for range total {
		v, ok := q.Dequeue()
		if !ok {
			t.Fatal("Dequeue returned ok=false before queue was drained")
		}
		if v < 0 || v >= total || seen[v] {
			t.Fatalf("Dequeue returned unexpected or duplicate value %d", v)
		}
		seen[v] = true
	}
}
