package memory

import (
	"slices"

	"github.com/Y2Kwastaken/model-citizen/shared"
)

// creates a new short term memory of size, size.
// short term memory rotates through a circular queue of size, size
// always evicting the oldest memories. By default short term memory
// can not have errors in the store function
func NewShortTermMemory(size int) (MemoryHolder, error) {
	queue, err := shared.NewCircularQueue[Memory](size)
	if err != nil {
		return nil, err
	}
	return &shortTermMemoryHolder{
		memories: queue,
	}, nil
}

// holds memories in memory and does no more than that
type shortTermMemoryHolder struct {
	memories shared.CircularQueue[Memory]
}

func (st *shortTermMemoryHolder) Store(memory Memory) (Memory, bool, error) {
	mem, ok := st.memories.Enqueue(memory)
	return mem, ok, nil
}

func (st *shortTermMemoryHolder) Ordered() []Memory {
	items := st.memories.Items()
	slices.SortFunc(items, Memory.Compare)
	return items
}

func (st *shortTermMemoryHolder) Wipe() {
	st.memories.Clear()
}
