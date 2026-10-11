package memory

func NewEmptyMemory() MemoryHolder {
	return &emptyMemoryHolder{}
}

// forgets everything it's given, for a model that doesn't remember
type emptyMemoryHolder struct {
}

func (st *emptyMemoryHolder) Store(memory Memory) (Memory, bool, error) {
	return Memory{}, false, nil
}

func (st *emptyMemoryHolder) Ordered() []Memory {
	return []Memory{}
}

func (st *emptyMemoryHolder) Wipe() {
}
