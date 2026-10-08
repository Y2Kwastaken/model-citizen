package main

import (
	"github.com/Y2Kwastaken/model-citizen/llm/config"
	"github.com/Y2Kwastaken/model-citizen/llm/model"
	"github.com/Y2Kwastaken/model-citizen/llm/server"
	"github.com/Y2Kwastaken/model-citizen/shared"
)

const (
	LLM_CONFIG_DEFAULT = "config/llm.toml"
	LLM_CONFIG_ENV     = "LLM_CONFIG"
	LLM_ADDR_ENV       = "LLM_ADDR"
	LLM_ADDR_DEFAULT   = ":50051"
)

func main() {
	configFile := shared.EnvOrVal(LLM_CONFIG_ENV, LLM_CONFIG_DEFAULT)
	config, err := config.Load(configFile)
	if err != nil {
		panic(err)
	}

	brain, err := makeModel(config.Chat)
	if err != nil {
		panic(err)
	}
	registerTools(config, brain)

	err = server.StartGrpc(brain, config, shared.EnvOrVal(LLM_ADDR_ENV, LLM_ADDR_DEFAULT))
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

func registerTools(config config.Config, brain model.LanguageModel) {
	tools := brain.ToolSet()
	tools.Add(model.MemorizeTool(brain, config.Chat.HistorySize))
	tools.Add(model.SwitchPersonalityTool(brain))
}
