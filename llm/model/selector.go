package model

import (
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

const (
	// a verdict, and the deadline a single request is given
	kill_threshold   = 10 * time.Second
	reward_threshold = 2 * time.Second
	punish_threshold = 5 * time.Second

	skip_score = 10

	// What a service that needs no key at all -- one running locally, say --
	// puts in auth_key, rather than naming a variable that will never be set.
	noAuthKey = "NOP"

	// an endpoint being down is nearly always temporary, so a bench expires
	parole_period = 5 * time.Minute
)

type ModelManager interface {
	// Model reports the model requests should be sent to right now.
	Model() Model
	// Judge records how long a completed request took, and swaps away from a
	// model once it has been slow often enough to earn it.
	Judge(model Model, latency time.Duration)
	// Fail benches a model whose request errored outright and swaps off it.
	Fail(model Model, err error)
	// Rotate forces a swap to the next model.
	Rotate()
	// Count is how many failover attempts one request is worth.
	Count() int
}

type ModelProvider struct {
	lock     sync.RWMutex
	selected int
	models   []Model
}

type Model struct {
	Client openai.Client
	Name   string
	// Transcribe and Speak are set only for a service with its own API; nil
	// means the client above speaks OpenAI's shape.
	Transcribe Transcriber
	Speak      Speaker
	// position in the provider's slice
	Index int
	score int
	// time since out of rotation
	benched time.Time
}

type jsonModel struct {
	Name    string `json:"name"`
	BaseUrl string `json:"base_url"`
	AuthKey string `json:"auth_key"`
	// Api is the shape the service speaks, defaulting to OpenAI's.
	Api string `json:"api"`
	// Voice is which of a speech service's voices to use. Speech is the only
	// feature where the model and the voice are separate things.
	Voice string `json:"voice"`
}

// NewModelManager builds a rotation from modelsFile for one feature. The
// feature is what every service in the file is checked against, so a roster
// that cannot do the job it was listed for fails here.
func NewModelManager(modelsFile string, feature ModelFeature) (ModelManager, error) {
	dataModels, err := readModelsFile(modelsFile)
	if err != nil {
		return nil, err
	}
	return newModelProvider(dataModels, feature)
}

func readModelsFile(modelsFile string) ([]jsonModel, error) {
	file, err := os.Open(modelsFile)
	if err != nil {
		return nil, fmt.Errorf("opening models file: %w", err)
	}
	defer file.Close()

	var models []jsonModel
	if err := json.UnmarshalRead(file, &models); err != nil {
		return nil, fmt.Errorf("reading models file: %w", err)
	}

	if len(models) == 0 {
		return nil, fmt.Errorf("no models listed in %s", modelsFile)
	}

	for i, model := range models {
		if model.Name == "" || model.BaseUrl == "" || model.AuthKey == "" {
			return nil, fmt.Errorf("model %d in %s is missing name, base_url or auth_key", i, modelsFile)
		}
	}

	return models, nil
}

// newModelProvider resolves each auth_key against the environment and builds a
// client per model. A model whose key is unset is dropped rather than fatal, so
// the roster can list a service ahead of having credentials for it; a model
// that asks for no key with noAuthKey is kept as it is.
func newModelProvider(dataModels []jsonModel, feature ModelFeature) (*ModelProvider, error) {
	models := make([]Model, 0, len(dataModels))
	for _, modelData := range dataModels {
		var authKey string
		if modelData.AuthKey != noAuthKey {
			authKey = os.Getenv(modelData.AuthKey)
			if authKey == "" {
				slog.Warn("skipping model, its key is not set in the environment",
					slog.String("model", modelData.Name),
					slog.String("auth_key", modelData.AuthKey),
				)
				continue
			}
		}

		transcribe, speak, err := adaptersFor(feature, modelData.Api, service{
			baseUrl: modelData.BaseUrl,
			authKey: authKey,
			name:    modelData.Name,
			voice:   modelData.Voice,
		})
		if err != nil {
			return nil, fmt.Errorf("model %s: %w", modelData.Name, err)
		}

		models = append(models, Model{
			Client: openai.NewClient(
				option.WithBaseURL(modelData.BaseUrl),
				option.WithAPIKey(authKey),
				// we have our own retry policy
				option.WithMaxRetries(0),
			),
			Name:       modelData.Name,
			Transcribe: transcribe,
			Speak:      speak,
			Index:      len(models),
		})
	}

	if len(models) == 0 {
		return nil, fmt.Errorf("no models to rotate through: every model listed is missing its key in the environment")
	}

	slog.Info("model rotation ready",
		slog.Int("models", len(models)),
		slog.String("starting", models[0].Name),
	)

	return &ModelProvider{
		selected: 0,
		models:   models,
	}, nil
}

func (provider *ModelProvider) Model() Model {
	provider.lock.RLock()
	defer provider.lock.RUnlock()
	return *currentModel(provider)
}

func (provider *ModelProvider) Count() int {
	provider.lock.RLock()
	defer provider.lock.RUnlock()
	return len(provider.models)
}

func (provider *ModelProvider) Judge(model Model, latency time.Duration) {
	provider.lock.Lock()
	defer provider.lock.Unlock()

	judged := &provider.models[model.Index]
	switch {
	case latency >= kill_threshold:
		judged.score = skip_score
	case latency <= reward_threshold:
		// floor so no good favor is built
		judged.score = max(judged.score-1, 0)
	case latency >= punish_threshold:
		judged.score += 2
	}

	if judged.score < skip_score {
		return
	}
	judged.benched = time.Now()

	if model.Index != provider.selected {
		return
	}
	nextModel(provider)
}

func (provider *ModelProvider) Fail(model Model, err error) {
	provider.lock.Lock()
	defer provider.lock.Unlock()

	failed := &provider.models[model.Index]
	failed.score = skip_score
	failed.benched = time.Now()
	slog.Warn("benching model", slog.String("model", failed.Name), slog.Any("error", err))

	if model.Index == provider.selected {
		nextModel(provider)
	}
}

func (provider *ModelProvider) Rotate() {
	provider.lock.Lock()
	defer provider.lock.Unlock()
	nextModel(provider)
}

func currentModel(provider *ModelProvider) *Model {
	return &provider.models[provider.selected]
}

// nextModel moves selection to the next model that is not benched, wrapping
// around. The caller must hold the write lock.
func nextModel(provider *ModelProvider) {
	length := len(provider.models)
	from := provider.models[provider.selected].Name
	now := time.Now()

	for i := 1; i <= length; i++ {
		candidate := &provider.models[(provider.selected+i)%length]
		if candidate.score >= skip_score {
			if now.Sub(candidate.benched) < parole_period {
				continue
			}
			candidate.score = 0 // paroled, it gets judged fresh from here
		}

		provider.selected = candidate.Index
		slog.Info("swapping model", slog.String("from", from), slog.String("to", candidate.Name))
		return
	}

	// nothing is healthy and nothing has served its parole, take longest benched
	stalest := 0
	for i := range provider.models {
		if provider.models[i].benched.Before(provider.models[stalest].benched) {
			stalest = i
		}
	}

	provider.models[stalest].score = 0
	provider.selected = stalest
	slog.Warn("every model is benched, paroling the stalest",
		slog.String("from", from),
		slog.String("to", provider.models[stalest].Name),
	)
}
