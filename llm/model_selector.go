package llm

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
	kill_threshold   = 10 * time.Second
	reward_threshold = 2 * time.Second
	punish_threshold = 5 * time.Second

	skip_score = 10

	// A benched model comes back up for selection after this long. An endpoint
	// being down is nearly always temporary, and without parole one bad minute
	// costs us that model for the life of the process.
	parole_period = 5 * time.Minute
)

type ModelManager interface {
	// Model reports the model requests should be sent to right now.
	Model() Model
	// Judge records how long a *completed* request took on model, and swaps
	// away from it once it has been slow often enough to earn it.
	Judge(model Model, latency time.Duration)
	// Fail benches a model whose request errored outright and swaps off it.
	Fail(model Model, err error)
	// Rotate forces a swap to the next model.
	Rotate()
	// Count reports how many models are in the rotation, which is how many
	// failover attempts a single request is worth.
	Count() int
}

type ModelProvider struct {
	lock     sync.RWMutex
	selected int
	models   []Model
}

type Model struct {
	client openai.Client
	name   string
	score  int
	// when this model was last taken out of rotation, for parole
	benched time.Time
	// position in the provider's slice, so a verdict can be attributed back to
	// the model that earned it even if another goroutine rotated in between
	index int
}

type jsonModel struct {
	Name    string `json:"name"`
	BaseUrl string `json:"base_url"`
	// the *name* of the environment variable holding this model's key, never
	// the key itself: models.json is committed, data/.env is not
	AuthKey string `json:"auth_key"`
}

// newModelManager builds the rotation from modelsFile, falling back to the
// single model named by the environment when the file is missing or unusable.
func newModelManager(modelsFile string, modelAuthKey string, modelNameKey string, modelLinkKey string) (ModelManager, error) {
	dataModels, err := readModelsFile(modelsFile)
	if err != nil {
		slog.Warn("falling back to the single model in the environment",
			slog.String("file", modelsFile),
			slog.Any("error", err),
		)

		dataModels, err = newJsonModelFromEnvironment(modelAuthKey, modelNameKey, modelLinkKey)
		if err != nil {
			return nil, err
		}
	}

	return newModelProvider(dataModels)
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

// newModelProvider resolves each model's auth_key against the environment and
// builds a client per model. Keys stay internal to this package: the roster
// names variables, this is the only place their values are read.
//
// A model whose key is not set is dropped rather than fatal, so the roster can
// list a service ahead of having credentials for it.
func newModelProvider(dataModels []jsonModel) (*ModelProvider, error) {
	models := make([]Model, 0, len(dataModels))
	for _, modelData := range dataModels {
		authKey := os.Getenv(modelData.AuthKey)
		if authKey == "" {
			slog.Warn("skipping model, its key is not set in the environment",
				slog.String("model", modelData.Name),
				slog.String("auth_key", modelData.AuthKey),
			)
			continue
		}

		models = append(models, Model{
			client: openai.NewClient(
				option.WithBaseURL(modelData.BaseUrl),
				option.WithAPIKey(authKey),
				// The rotation is the retry policy. The client's own retries
				// run *inside* the per-model deadline with up to 8s of backoff,
				// which spends this model's whole slice waiting and swallows
				// the status code that explains why.
				option.WithMaxRetries(0),
			),
			name:  modelData.Name,
			index: len(models),
		})
	}

	if len(models) == 0 {
		return nil, fmt.Errorf("no models to rotate through: every model listed is missing its key in the environment")
	}

	slog.Info("model rotation ready",
		slog.Int("models", len(models)),
		slog.String("starting", models[0].name),
	)

	return &ModelProvider{
		selected: 0,
		models:   models,
	}, nil
}

func newJsonModelFromEnvironment(modelAuthKey string, modelNameKey string, modelLinkKey string) ([]jsonModel, error) {
	modelName := os.Getenv(modelNameKey)
	if modelName == "" {
		return nil, fmt.Errorf("model name did not exist in environment at: %s", modelNameKey)
	}

	modelLink := os.Getenv(modelLinkKey)
	if modelLink == "" {
		return nil, fmt.Errorf("model link did not exist in environment at: %s", modelLinkKey)
	}

	return []jsonModel{
		{
			Name:    modelName,
			BaseUrl: modelLink,
			AuthKey: modelAuthKey,
		},
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

	judged := &provider.models[model.index]
	switch {
	case latency >= kill_threshold:
		judged.score = skip_score
	case latency <= reward_threshold:
		// floored, so a model with a good morning cannot bank enough credit to
		// ride out a bad afternoon
		judged.score = max(judged.score-1, 0)
	case latency >= punish_threshold:
		judged.score += 2
	}

	if judged.score < skip_score {
		return
	}
	judged.benched = time.Now()

	// A verdict from a request that started before the last swap still counts
	// against its own model, but it does not get to move the rotation again.
	if model.index != provider.selected {
		return
	}
	nextModel(provider)
}

func (provider *ModelProvider) Fail(model Model, err error) {
	provider.lock.Lock()
	defer provider.lock.Unlock()

	failed := &provider.models[model.index]
	failed.score = skip_score
	failed.benched = time.Now()
	slog.Warn("benching model", slog.String("model", failed.name), slog.Any("error", err))

	if model.index == provider.selected {
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
	from := provider.models[provider.selected].name
	now := time.Now()

	for i := 1; i <= length; i++ {
		candidate := &provider.models[(provider.selected+i)%length]
		if candidate.score >= skip_score {
			if now.Sub(candidate.benched) < parole_period {
				continue
			}
			candidate.score = 0 // paroled, it gets judged fresh from here
		}

		provider.selected = candidate.index
		slog.Info("swapping model", slog.String("from", from), slog.String("to", candidate.name))
		return
	}

	// Nothing is healthy and nothing has served its parole. Take whichever has
	// been benched longest rather than strand the bot on a model we already
	// gave up on — staying put would just hammer the same dead endpoint.
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
		slog.String("to", provider.models[stalest].name),
	)
}
