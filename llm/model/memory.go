package model

import (
	"slices"
	"sync"
	"time"
)

type MemoryType int

const (
	SHORT_TERM MemoryType = iota
	LONG_TERM
)
const defaultShortTermSize int = 10

func (memType MemoryType) String() string {
	if memType == LONG_TERM {
		return "long term"
	}
	return "short term"
}

type ModelMemory struct {
	At     time.Time
	Name   string
	Memory string
}

type MemorySet interface {
	Insert(memory ModelMemory, memType MemoryType) bool
	OrderedMemories(memType MemoryType) []ModelMemory
	AllOrderedMemories() []ModelMemory
	Wipe(memType MemoryType)
}

type MemoryProvider interface {
	Insert(memory ModelMemory) bool
	OrderedMemories() []ModelMemory
	Wipe()
}

// A fixed size window of the most recent memories, oldest dropped first
type shortTermMemory struct {
	lock     sync.Mutex
	head     int
	tail     int
	empty    bool
	maxSize  int
	memories []ModelMemory
}

// Creates a new short term memory of size n
func NewShortTerm(size int) MemoryProvider {
	if size <= 0 {
		size = defaultShortTermSize
	}

	return &shortTermMemory{
		head:     0,
		tail:     0,
		empty:    true,
		maxSize:  size,
		memories: make([]ModelMemory, size),
	}
}

// Stores a memory, reporting false when an older memory had to be dropped to make room
func (short *shortTermMemory) Insert(memory ModelMemory) bool {
	short.lock.Lock()
	defer short.lock.Unlock()

	if memory.At.IsZero() {
		memory.At = time.Now()
	}

	evicted := short.head == short.tail && !short.empty
	if evicted {
		short.tail = (short.tail + 1) % short.maxSize
	}

	short.memories[short.head] = memory
	short.head = (short.head + 1) % short.maxSize
	short.empty = false

	return !evicted
}

// Every remembered memory, oldest first
func (short *shortTermMemory) OrderedMemories() []ModelMemory {
	short.lock.Lock()
	defer short.lock.Unlock()

	if short.empty {
		return []ModelMemory{}
	}

	// head == tail means the buffer is full, every other case is head running ahead of tail
	count := short.head - short.tail
	if count <= 0 {
		count += short.maxSize
	}

	ordered := make([]ModelMemory, 0, count)
	for i := 0; i < count; i++ {
		ordered = append(ordered, short.memories[(short.tail+i)%short.maxSize])
	}

	return ordered
}

func (short *shortTermMemory) Wipe() {
	short.lock.Lock()
	defer short.lock.Unlock()

	short.head = 0
	short.tail = 0
	short.empty = true
	short.memories = make([]ModelMemory, short.maxSize)
}

// The memory types a set addresses, in the order AllOrderedMemories breaks ties
var memoryTypes = []MemoryType{SHORT_TERM, LONG_TERM}

// A provider per memory type, each one owning its own locking
type memorySet struct {
	providers map[MemoryType]MemoryProvider
}

func NewMemorySet(providers map[MemoryType]MemoryProvider) MemorySet {
	owned := make(map[MemoryType]MemoryProvider, len(providers))
	for memType, provider := range providers {
		if provider != nil {
			owned[memType] = provider
		}
	}

	return &memorySet{providers: owned}
}

// Stores a memory, reporting false when nothing backs the given type
func (set *memorySet) Insert(memory ModelMemory, memType MemoryType) bool {
	provider, ok := set.providers[memType]
	if ok {
		provider.Insert(memory)
	}
	return ok
}

func (set *memorySet) OrderedMemories(memType MemoryType) []ModelMemory {
	provider, ok := set.providers[memType]
	if !ok {
		return []ModelMemory{}
	}

	return provider.OrderedMemories()
}

// Every memory across every type, oldest first
func (set *memorySet) AllOrderedMemories() []ModelMemory {
	all := []ModelMemory{}
	for _, memType := range memoryTypes {
		if provider, ok := set.providers[memType]; ok {
			all = append(all, provider.OrderedMemories()...)
		}
	}

	// stable so memories sharing a timestamp stay in memoryTypes order
	slices.SortStableFunc(all, func(left ModelMemory, right ModelMemory) int {
		return left.At.Compare(right.At)
	})

	return all
}

func (set *memorySet) Wipe(memType MemoryType) {
	if provider, ok := set.providers[memType]; ok {
		provider.Wipe()
	}
}
