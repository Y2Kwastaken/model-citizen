package memory

import (
	"strings"
	"time"
)

type Identity int

const (
	NONE Identity = iota
	SELF
	OTHER
)

// a tool the model called while replying and what it returned
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
	Result    string
}

// Represents a given memory that is captured at a time
type Memory struct {
	// the time the memory is captured at
	CreatedAt time.Time
	// who the memory involves
	Involves []string
	// the actual content of the memory
	Content string
	// Identity bit data can be used instead of Involves for fine grained access
	// e.g. self identifying a model memory. This is also safe to not be filled
	Owner Identity
	// tool calls the model made before replying, only on SELF memories
	ToolCalls []ToolCall
}

func (m Memory) Compare(other Memory) int {
	return m.CreatedAt.Compare(other.CreatedAt)
}

// full string with string involved parties and content
func (m *Memory) String() string {
	return m.CreatedAt.Format(time.ANSIC) + " Involves[" + strings.Join(m.Involves, ",") + "]: " + m.Content
}

// string with just time and content
func (m *Memory) StringSimple() string {
	return m.CreatedAt.Format(time.ANSIC) + ": " + m.Content
}

// a general holder of memories
type MemoryHolder interface {
	// stores a given memory returns a memory and true if a memory was evicted
	// or an error if an error occurred in the store process
	Store(memory Memory) (Memory, bool, error)
	// an array of memories in order from oldest to newest
	Ordered() []Memory
	// wipes the memory holder completely
	Wipe()
}
