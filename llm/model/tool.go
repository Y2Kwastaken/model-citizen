package model

import (
	"context"
	"log/slog"
	"maps"
	"slices"

	"github.com/disgoorg/snowflake/v2"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

type Origin struct {
	Guild   snowflake.ID
	Channel snowflake.ID
	Caller  snowflake.ID
}

type Invocation struct {
	Origin
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
	CallTool(ctx context.Context, function openai.ChatCompletionMessageFunctionToolCallFunction, origin Origin) string
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

func (provider *modelToolProvider) CallTool(ctx context.Context, function openai.ChatCompletionMessageFunctionToolCallFunction, origin Origin) string {
	name := function.Name
	tool, ok := provider.Lookup(name)
	if !ok {
		slog.Warn("model called a tool that does not exist", slog.String("tool", name))
		return "there is no tool called " + name
	}

	slog.Info("running tool",
		slog.String("tool", name),
		slog.String("arguments", function.Arguments),
		slog.String("guild_id", origin.Guild.String()),
	)

	result := tool.Handle(ctx, Invocation{Origin: origin, Arguments: function.Arguments})
	return result
}
