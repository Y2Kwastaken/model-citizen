package model

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Y2Kwastaken/model-citizen/llm/memory"
	"github.com/Y2Kwastaken/model-citizen/llm/tools"
	"github.com/Y2Kwastaken/model-citizen/llm/util"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

func MemorizeTool(brain LanguageModel, historySize int) tools.Tool {
	return tools.Tool{
		Function: shared.FunctionDefinitionParam{
			Name:        "memorize",
			Description: openai.String("keep memory, memorize, or store to short term memory"),
			Parameters: shared.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"name":   map[string]any{"type": "string", "description": "who the memory is about, their name or nickname"},
					"memory": map[string]any{"type": "string", "description": "the one line you are writing down, in your own words"},
				},
				"required": []string{"name", "memory"},
			},
		},
		Executor: func(ctx context.Context, src util.Source, args string) (string, error) {
			holder, ok := brain.Memory(src.Id, "short_term")
			if !ok {
				shortTerm, err := memory.NewShortTermMemory(historySize)
				if err != nil {
					return "", err
				}

				brain.AddMemoryHolder(src.Id, "short_term", shortTerm)
				holder, ok = brain.Memory(src.Id, "short_term")
				if !ok {
					return "", fmt.Errorf("failed to fetch short term memory after fetch and add")
				}
			}

			type memArgs struct {
				Who    string `json:"name"`
				Memory string `json:"memory"`
			}

			var parsed memArgs
			if err := json.Unmarshal([]byte(args), &parsed); err != nil {
				return "", err
			}

			_, _, err := holder.Store(memory.Memory{
				CreatedAt: time.Now(),
				Involves:  []string{parsed.Who},
				Content:   parsed.Memory,
				Owner:     memory.NONE,
			})

			if err != nil {
				return "", err
			}

			return "stored memory", nil
		},
	}
}

func SwitchPersonalityTool(brain LanguageModel) tools.Tool {
	return tools.Tool{
		Function: shared.FunctionDefinitionParam{
			Name:        "personality",
			Description: openai.String("switches, altars, changes persona"),
			Parameters: shared.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "name of the personality"},
				},
				"required": []string{"name"},
			},
		},
		Executor: func(ctx context.Context, src util.Source, args string) (string, error) {
			type personalityArgs struct {
				Name string `json:"name"`
			}

			var parsed personalityArgs
			if err := json.Unmarshal([]byte(args), &parsed); err != nil {
				return "", err
			}

			ok := brain.SystemMemory(src.Id, parsed.Name)
			if !ok {
				return "", fmt.Errorf("could not set system meory to %s it was likely not found", parsed.Name)
			}

			return "set personality to" + parsed.Name, nil
		},
	}
}
