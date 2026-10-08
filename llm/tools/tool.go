package tools

import (
	"context"
	"fmt"
	"sync"

	"github.com/Y2Kwastaken/model-citizen/llm/util"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

type ToolExecutor func(ctx context.Context, src util.Source, args string) (string, error)

type Tool struct {
	Function shared.FunctionDefinitionParam
	Executor ToolExecutor
}

type ToolSet interface {
	Call(ctx context.Context, src util.Source, call openai.ChatCompletionMessageToolCallUnion) (string, error)
	Lookup(tool string) (ToolExecutor, error)
	Add(tool Tool) error
	AsOpenAI() []openai.FunctionDefinitionParam
}

func NewToolSet() ToolSet {
	return &toolSetHandler{
		tools: make(map[string]Tool),
	}
}

type toolSetHandler struct {
	lock  sync.RWMutex
	tools map[string]Tool
}

func (ts *toolSetHandler) Call(ctx context.Context, src util.Source, call openai.ChatCompletionMessageToolCallUnion) (string, error) {
	typ := call.Type
	if typ != "function" {
		return "", fmt.Errorf("tool call was not of type function was of type %s", typ)
	}

	functionCall := call.Function
	exec, err := ts.Lookup(functionCall.Name)
	if err != nil {
		return "", err
	}

	return exec(ctx, src, functionCall.Arguments)
}

func (ts *toolSetHandler) Lookup(tool string) (ToolExecutor, error) {
	ts.lock.RLock()
	toolFunc, ok := ts.tools[tool]
	ts.lock.RUnlock()
	if !ok {
		return nil, fmt.Errorf("tool %s not found", tool)
	}

	return toolFunc.Executor, nil
}

func (ts *toolSetHandler) Add(tool Tool) error {
	ts.lock.Lock()
	defer ts.lock.Unlock()

	if _, ok := ts.tools[tool.Function.Name]; ok {
		return fmt.Errorf("a tool with the name %s already exists", tool.Function.Name)
	}

	ts.tools[tool.Function.Name] = tool
	return nil
}

func (ts *toolSetHandler) AsOpenAI() []openai.FunctionDefinitionParam {
	ts.lock.RLock()
	defer ts.lock.RUnlock()

	slice := make([]openai.FunctionDefinitionParam, 0, len(ts.tools))
	for _, tool := range ts.tools {
		slice = append(slice, tool.Function)
	}

	return slice
}
