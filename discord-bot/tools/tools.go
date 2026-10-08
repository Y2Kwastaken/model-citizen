package tools

import (
	"context"
	"encoding/json"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/actions"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/network"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/snowflake/v2"
)

// ToolsFrom turns every action into a tool the llm can call.
func ToolsFrom(all []actions.Action, client *bot.Client) []network.Tool {
	tools := make([]network.Tool, len(all))
	for i, action := range all {
		tools[i] = toolFrom(action, client)
	}
	return tools
}

func toolFrom(action actions.Action, client *bot.Client) network.Tool {
	return network.Tool{
		Name:        action.Name,
		Description: action.Description,
		Parameters:  schema(action.Params),
		Run:         toolHandler(action, client),
	}
}

// the llm can't wait on slow work so actions run in the background where they can
func toolHandler(action actions.Action, client *bot.Client) func(context.Context, network.Call) (string, error) {
	return func(ctx context.Context, call network.Call) (string, error) {
		var raw map[string]any
		if call.Arguments != "" {
			if err := json.Unmarshal([]byte(call.Arguments), &raw); err != nil {
				return "", err
			}
		}

		args := make(actions.Args, len(raw))
		for name, value := range raw {
			if s, ok := value.(string); ok {
				args[name] = s
			}
		}

		return action.Run(ctx, actions.Request{
			Client:     client,
			Guild:      call.Guild,
			User:       callerId(call),
			Args:       args,
			Background: true,
		})
	}
}

// json schema for the params, nil for an action that takes none
func schema(params []actions.Param) map[string]any {
	if len(params) == 0 {
		return nil
	}

	properties := make(map[string]any, len(params))
	// []any since structpb only takes json shaped values
	required := []any{}
	for _, param := range params {
		properties[param.Name] = map[string]any{
			"type":        "string",
			"description": param.Description,
		}
		if param.Required {
			required = append(required, param.Name)
		}
	}

	return map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}

// the user who asked, zero when the llm didn't say so the voice checks find nobody
func callerId(call network.Call) snowflake.ID {
	id, err := snowflake.Parse(call.Who)
	if err != nil {
		return 0
	}
	return id
}
