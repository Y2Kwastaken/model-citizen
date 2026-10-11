package main

import (
	"github.com/Y2Kwastaken/model-citizen/llm/config"
	"github.com/Y2Kwastaken/model-citizen/llm/model"
	"github.com/Y2Kwastaken/model-citizen/llm/server"
	"github.com/Y2Kwastaken/model-citizen/shared"
)

const (
	LLM_CONFIG_DEFAULT = "config/llm"
	LLM_CONFIG_ENV     = "LLM_CONFIG"
	LLM_ADDR_ENV       = "LLM_ADDR"
	LLM_ADDR_DEFAULT   = ":50051"
)

func main() {
	configDir := shared.EnvOrVal(LLM_CONFIG_ENV, LLM_CONFIG_DEFAULT)
	config, err := config.Load(configDir)
	if err != nil {
		panic(err)
	}

	brain, err := makeModel(config.Chat)
	if err != nil {
		panic(err)
	}

	// nil without tools.toml, then serious tools go to the brain
	var toolBrain model.LanguageModel
	if config.Tools.Enabled() {
		toolBrain, err = makeToolModel(config.Tools)
		if err != nil {
			panic(err)
		}
	}
	registerTools(config, brain, toolBrain)

	err = server.StartGrpc(brain, toolBrain, config, shared.EnvOrVal(LLM_ADDR_ENV, LLM_ADDR_DEFAULT))
	if err != nil {
		panic(err)
	}
}

func makeModel(chat config.Chat) (model.LanguageModel, error) {
	return model.NewBrainModel(
		chat.HistorySize,
		chat.Prompts(),
		chat.ChatClients(),
		chat.Rotation.Policy(),
		[]model.FeatureFlag{model.CHAT, model.MEMORY, model.TOOL},
	)
}

// a brain that only calls tools, it has no memory so it needs no history size
func makeToolModel(tools config.Tools) (model.LanguageModel, error) {
	return model.NewBrainModel(
		0,
		map[string]string{model.ToolPrompt: tools.Prompt},
		tools.ChatClients(),
		tools.Rotation.Policy(),
		[]model.FeatureFlag{model.TOOL},
	)
}

func registerTools(config config.Config, brain model.LanguageModel, toolBrain model.LanguageModel) {
	tools, err := brain.ToolSet()
	if err != nil {
		panic(err)
	}

	if brain.HasFeature(model.MEMORY) {
		tools.Add(model.MemorizeTool(brain, config.Chat.HistorySize))
	}
	tools.Add(model.SwitchPersonalityTool(brain))
	if toolBrain != nil {
		tools.Add(model.HandoffTool(toolBrain))
	}
}
