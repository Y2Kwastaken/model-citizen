package model

import (
	"context"

	"github.com/Y2Kwastaken/model-citizen/llm/memory"
	"github.com/Y2Kwastaken/model-citizen/llm/tools"
	"github.com/Y2Kwastaken/model-citizen/llm/util"
)

type LanguageModel interface {
	// Features

	// checks whether or not a language model supports this feature
	HasFeature(flag FeatureFlag) bool
	// a slice of all features
	Features() []FeatureFlag

	// Tools

	// gets the tool set this language model provides
	ToolSet() tools.ToolSet

	// Memory

	// fetches a memory holder from the given id and string id coordinates
	// each id has it's own set of string sub ids. e.g.
	// 1,a wouldn't clash with 2,a and so on.
	Memory(id int64, sid string) (memory.MemoryHolder, bool)

	// adds a memory h older to the given coordinates
	// returns true if a memory holder was added, otherwise false.
	AddMemoryHolder(id int64, sid string, holder memory.MemoryHolder) bool

	// sets the "system memory" aka system prompt for a given model
	// each system prompt set is specific to a certain id and the sid
	// is the system prompt id. True if the memory was switched.
	// Note there should exist no crossover between user memories and system
	// memories they are inherintly different exchanges
	SystemMemory(id int64, sid string) bool

	// the sid of the system memory set for id, false if none has been set
	SystemMemoryName(id int64) (string, bool)

	// Actions

	// Creates a chat from a ctx and source information.
	// A nil source means there exists no source.
	// No source means no memories or system memories are applied to the response
	Chat(ctx context.Context, source util.Source) (string, error)
}
