package llm

import (
	"os"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

type BrainClient interface {
}

type BasicClientProvider struct {
	client openai.Client
	model  string
}

func NewBrainClient(modelAuthKey string, modelName string) (BrainClient, error) {
	client := openai.NewClient(
		option.WithBaseURL("https://integrate.api.nvidia.com/v1"),
		option.WithAPIKey(os.Getenv(modelAuthKey)),
	)

	provider := BasicClientProvider{
		client: client,
		model:  modelName,
	}

	return provider, nil
}
