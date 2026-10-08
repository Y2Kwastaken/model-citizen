package model

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Y2Kwastaken/model-citizen/llm/clients"
	"github.com/Y2Kwastaken/model-citizen/llm/memory"
	"github.com/Y2Kwastaken/model-citizen/llm/tools"
	"github.com/Y2Kwastaken/model-citizen/llm/util"
	"github.com/Y2Kwastaken/model-citizen/shared"
	"github.com/openai/openai-go/v3"
)

// rounds of tool calls in one reply before tools are taken away
const maxToolRounds = 2

type brainModel struct {
	chatters shared.Rotation[clients.ChatClient]
	// capacity of each channel's message history
	historySize int

	// tools
	tools tools.ToolSet

	// data
	flock sync.RWMutex
	flags map[FeatureFlag]struct{}

	mlock  sync.RWMutex
	memory map[int64]*memorySection

	smlock         sync.Mutex
	systemMemories map[string]memory.MemoryHolder
	systemMemory   map[int64]memory.Memory
	// the sid of each id's system memory
	systemNames map[int64]string
}

type memorySection struct {
	lock    sync.RWMutex
	holders map[string]memory.MemoryHolder
}

// INIT

func NewBrainModel(historySize int, systemPrompts map[string]string, chatters []clients.ChatClient, policy shared.RotationPolicy, flags []FeatureFlag) (LanguageModel, error) {
	chatRotation, err := shared.NewRotation(chatters, policy)
	if err != nil {
		return nil, err
	}

	flagSet := make(map[FeatureFlag]struct{})
	for _, flag := range flags {
		flagSet[flag] = struct{}{}
	}

	systemMemories := make(map[string]memory.MemoryHolder)
	for name, fileName := range systemPrompts {
		prompt, err := memory.NewSystemMemory(fileName)
		if err != nil {
			return nil, err
		}

		systemMemories[name] = prompt
	}

	return &brainModel{
		chatters:    chatRotation,
		historySize: historySize,

		tools:          tools.NewToolSet(),
		flags:          flagSet,
		memory:         make(map[int64]*memorySection),
		systemMemories: systemMemories,
		systemMemory:   make(map[int64]memory.Memory),
		systemNames:    make(map[int64]string),
	}, nil
}

// FEATURES

func (m *brainModel) HasFeature(flag FeatureFlag) bool {
	m.flock.RLock()
	defer m.flock.RUnlock()
	_, ok := m.flags[flag]
	return ok
}

func (m *brainModel) Features() []FeatureFlag {
	m.flock.RLock()
	defer m.flock.RUnlock()

	out := make([]FeatureFlag, 0, len(m.flags))
	out = slices.AppendSeq(out, maps.Keys(m.flags))
	return out
}

// TOOLS

func (m *brainModel) ToolSet() tools.ToolSet {
	return m.tools
}

// MEMORY

func (m *brainModel) Memory(id int64, sid string) (memory.MemoryHolder, bool) {
	m.mlock.RLock()
	section, ok := m.memory[id]
	m.mlock.RUnlock()
	if !ok {
		return nil, false
	}

	section.lock.RLock()
	defer section.lock.RUnlock()
	holder, ok := section.holders[sid]
	if !ok {
		return nil, false
	}

	return holder, true
}

func (m *brainModel) AddMemoryHolder(id int64, sid string, holder memory.MemoryHolder) bool {
	m.mlock.Lock()
	defer m.mlock.Unlock()
	section, ok := m.memory[id]
	if ok {
		section.lock.Lock()
		defer section.lock.Unlock()
		_, ok := section.holders[sid]
		if ok {
			return false
		}

		section.holders[sid] = holder
		return true
	}

	m.memory[id] = &memorySection{
		holders: map[string]memory.MemoryHolder{sid: holder},
	}
	return true
}

func (m *brainModel) SystemMemory(id int64, sid string) bool {
	m.smlock.Lock()
	defer m.smlock.Unlock()
	srcMemory, ok := m.systemMemories[sid]
	if !ok {
		return false
	}

	m.systemMemory[id] = srcMemory.Ordered()[0]
	m.systemNames[id] = sid
	return true
}

func (m *brainModel) SystemMemoryName(id int64) (string, bool) {
	m.smlock.Lock()
	defer m.smlock.Unlock()
	sid, ok := m.systemNames[id]
	return sid, ok
}

func (m *brainModel) Chat(ctx context.Context, source util.Source) (string, error) {
	if !m.HasFeature(CHAT) {
		return "", fmt.Errorf("this brain does not support chatting")
	}

	history, err := m.history(source)
	if err != nil {
		return "", err
	}

	if m.chatters.Len() == 0 {
		return "", fmt.Errorf("no chat supported models in rotation")
	}

	entry := m.chatters.Current()
	current := entry.Value

	params := current.Params(m.messages(source.Id, history.Ordered()))
	toolParams := m.toolParams()

	var calls []memory.ToolCall
	var reply string
	ids := make(map[string]struct{})
	// time spent waiting on the model and on tools, the rest of a reply is ours
	var modelTime, toolTime time.Duration
	rounds, toolRounds, asked := 0, 0, 1
	// why the last completion ended and how long it was, stop vs length tells
	// a model choosing to say nothing apart from one cut off by the token cap
	var finish string
	var tokens int64
	for {
		rounds++
		if toolRounds < maxToolRounds {
			params.Tools = toolParams
		} else {
			params.Tools = nil
		}

		start := time.Now()
		completion, err := current.Client.Chat.Completions.New(ctx, params)
		modelTime += time.Since(start)
		if err != nil {
			return "", err
		}

		if len(completion.Choices) == 0 {
			return "", fmt.Errorf("chat completion returned no choices")
		}

		message := completion.Choices[0].Message
		finish, tokens = completion.Choices[0].FinishReason, completion.Usage.CompletionTokens
		if len(message.ToolCalls) == 0 {
			reply = strings.TrimSpace(message.Content)

			// some models decline by saying nothing at all, so the next model in
			// the rotation picks up the conversation from here instead
			if reply == "" && asked < m.chatters.Len() {
				slog.Warn("empty chat reply, asking the next model",
					slog.String("model", current.Settings().Model),
					slog.String("finish_reason", finish),
					slog.Int64("tokens", tokens),
				)
				m.chatters.Fail(entry)
				entry = m.chatters.Current()
				current = entry.Value
				params = current.Params(params.Messages)
				asked++
				continue
			}
			break
		}
		toolRounds++

		params.Messages = append(params.Messages, message.ToParam())
		for _, call := range message.ToolCalls {
			start := time.Now()
			result := m.callTool(ctx, source, call)
			toolTime += time.Since(start)
			params.Messages = append(params.Messages, openai.ToolMessage(result, call.ID))

			id := call.ID
			if _, ok := ids[id]; ok {
				id = fmt.Sprintf("%s_%d", id, len(calls))
			}
			ids[id] = struct{}{}

			calls = append(calls, memory.ToolCall{
				ID:        id,
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
				Result:    result,
			})
		}
	}

	// shows the model's reply and tool call etc
	slog.Info("chat reply",
		slog.String("model", current.Settings().Model),
		slog.Int("tool_calls", len(calls)),
		slog.Int("rounds", rounds),
		slog.String("finish_reason", finish),
		slog.Int64("tokens", tokens),
		slog.Duration("model_time", modelTime),
		slog.Duration("tool_time", toolTime),
		slog.String("reply", reply),
	)

	// an empty reply isn't kept, the model would start copying it
	if reply == "" && len(calls) == 0 {
		return "", nil
	}
	if _, _, err := history.Store(memory.Memory{CreatedAt: time.Now(), Content: reply, Owner: memory.SELF, ToolCalls: calls}); err != nil {
		return "", err
	}

	return reply, nil
}

// the assistant message that made a reply's tool calls, rebuilt from history
func toolCallMessage(toolCalls []memory.ToolCall) openai.ChatCompletionMessageParamUnion {
	calls := make([]openai.ChatCompletionMessageToolCallUnionParam, len(toolCalls))
	for i, call := range toolCalls {
		calls[i] = openai.ChatCompletionMessageToolCallUnionParam{
			OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
				ID: call.ID,
				Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
					Name:      call.Name,
					Arguments: call.Arguments,
				},
			},
		}
	}

	assistant := openai.ChatCompletionAssistantMessageParam{ToolCalls: calls}
	return openai.ChatCompletionMessageParamUnion{OfAssistant: &assistant}
}

// the registered tools in the shape the chat api wants, nil when tools are off
func (m *brainModel) toolParams() []openai.ChatCompletionToolUnionParam {
	if !m.HasFeature(TOOL) {
		return nil
	}

	functions := m.tools.AsOpenAI()
	if len(functions) == 0 {
		return nil
	}

	out := make([]openai.ChatCompletionToolUnionParam, 0, len(functions))
	for _, function := range functions {
		out = append(out, openai.ChatCompletionFunctionTool(function))
	}
	return out
}

func (m *brainModel) callTool(ctx context.Context, source util.Source, call openai.ChatCompletionMessageToolCallUnion) string {
	start := time.Now()
	result, err := m.tools.Call(ctx, source, call)
	took := time.Since(start)
	if err != nil {
		slog.Warn("tool call failed", slog.String("tool", call.Function.Name), slog.String("arguments", call.Function.Arguments), slog.Duration("took", took), slog.Any("error", err))
		return "error: " + err.Error()
	}

	slog.Info("tool called", slog.String("tool", call.Function.Name), slog.String("arguments", call.Function.Arguments), slog.Duration("took", took), slog.String("result", result))
	return result
}

// fetches the message history for the source's channel, creating a short term memory for it on first use
func (m *brainModel) history(src util.Source) (memory.MemoryHolder, error) {
	if holder, ok := m.Memory(src.Id, src.Where); ok {
		return holder, nil
	}

	holder, err := memory.NewShortTermMemory(m.historySize)
	if err != nil {
		return nil, err
	}

	// another goroutine may have added a history first, either way fetch whatever got stored
	m.AddMemoryHolder(src.Id, src.Where, holder)
	holder, _ = m.Memory(src.Id, src.Where)
	return holder, nil
}

// builds the chat messages, system prompt first then the history oldest to newest
func (m *brainModel) messages(id int64, history []memory.Memory) []openai.ChatCompletionMessageParamUnion {
	out := make([]openai.ChatCompletionMessageParamUnion, 0, len(history)+5)

	m.smlock.Lock()
	system, ok := m.systemMemory[id]
	m.smlock.Unlock()
	if ok {
		out = append(out, openai.SystemMessage(system.Content))
	}

	// short term
	if holder, ok := m.Memory(id, "short_term"); ok {
		for _, mem := range holder.Ordered() {
			content := mem.Content
			if len(mem.Involves) > 0 {
				content = "note about: " + mem.Involves[0] + ": " + content
			}

			out = append(out, openai.SystemMessage(content))
		}
	}
	// end short term

	for _, mem := range history {
		if mem.Owner == memory.SELF {
			// a reply that used tools expands back into the calls, their results, then the reply
			if len(mem.ToolCalls) > 0 {
				out = append(out, toolCallMessage(mem.ToolCalls))
				for _, call := range mem.ToolCalls {
					out = append(out, openai.ToolMessage(call.Result, call.ID))
				}
			}

			out = append(out, openai.AssistantMessage(mem.Content))
			continue
		}

		content := mem.Content
		if len(mem.Involves) > 0 {
			content = mem.Involves[0] + ": " + content
		}
		out = append(out, openai.UserMessage(content))
	}

	return out
}
