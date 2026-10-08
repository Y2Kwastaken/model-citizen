package memory

import (
	"os"
	"time"
)

// reads in a sytem memory from a file
func NewSystemMemory(fileName string) (MemoryHolder, error) {
	sysPrompt, err := os.ReadFile(fileName)
	if err != nil {
		return nil, err
	}

	return &systemMemoryHolder{
		instructionMemory: Memory{
			CreatedAt: time.Now(),
			Involves:  []string{},
			Content:   string(sysPrompt),
		},
	}, nil
}

type systemMemoryHolder struct {
	instructionMemory Memory
}

func (sm *systemMemoryHolder) Store(memory Memory) (Memory, bool, error) {
	return Memory{}, false, nil
}

func (sm *systemMemoryHolder) Ordered() []Memory {
	return []Memory{sm.instructionMemory}
}

func (sm *systemMemoryHolder) Wipe() {
}
