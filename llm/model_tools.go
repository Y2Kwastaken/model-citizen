package llm

import (
	"context"
	"maps"
	"slices"

	"github.com/disgoorg/snowflake/v2"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

// Origin is where a message came from: the room, and who the model is
// answering.
type Origin struct {
	Guild   snowflake.ID
	Channel snowflake.ID
	Caller  snowflake.ID
}

// Invocation is one tool call the model made, and where it came from.
type Invocation struct {
	Origin
	// the arguments as the model wrote them: json, usually valid
	Arguments string
}

type ToolHandler func(ctx context.Context, call Invocation) string

type Tool struct {
	Definition shared.FunctionDefinitionParam
	Handle     ToolHandler
}

type ModelTools interface {
	Build() []openai.ChatCompletionToolUnionParam
	Register(tool Tool)
	Lookup(name string) (Tool, bool)
}

type modelToolProvider struct {
	byName map[string]Tool
}

func NewModelTools() ModelTools {
	return &modelToolProvider{
		byName: make(map[string]Tool),
	}
}

func (provider *modelToolProvider) Build() []openai.ChatCompletionToolUnionParam {
	built := make([]openai.ChatCompletionToolUnionParam, 0, len(provider.byName))
	for _, name := range slices.Sorted(maps.Keys(provider.byName)) {
		built = append(built, openai.ChatCompletionFunctionTool(provider.byName[name].Definition))
	}
	return built
}

func (provider *modelToolProvider) Register(tool Tool) {
	provider.byName[tool.Definition.Name] = tool
}

func (provider *modelToolProvider) Lookup(name string) (Tool, bool) {
	tool, ok := provider.byName[name]
	return tool, ok
}
