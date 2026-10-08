package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// how often a queued AssemblyAI transcript is checked on
const assemblyAIPoll = 250 * time.Millisecond

// AssemblyAI has no synchronous endpoint: the clip is uploaded, queued, then
// polled until it's done, so it belongs last in the rotation
type AssemblyAITranscriber struct {
	baseURL string
	key     string
	model   string
}

func NewAssemblyAITranscriber(baseURL string, key string, model string) *AssemblyAITranscriber {
	return &AssemblyAITranscriber{baseURL: baseURL, key: key, model: model}
}

func (t *AssemblyAITranscriber) Model() string {
	return t.model
}

func (t *AssemblyAITranscriber) Transcribe(ctx context.Context, wav []byte, language string) (string, error) {
	uploaded, err := t.upload(ctx, wav)
	if err != nil {
		return "", fmt.Errorf("uploading clip: %w", err)
	}

	id, err := t.queue(ctx, uploaded, language)
	if err != nil {
		return "", fmt.Errorf("queueing clip: %w", err)
	}

	return t.await(ctx, id)
}

func (t *AssemblyAITranscriber) upload(ctx context.Context, wav []byte) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL+"/v2/upload", bytes.NewReader(wav))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", t.key)
	request.Header.Set("Content-Type", "audio/wav")

	var answer struct {
		UploadURL string `json:"upload_url"`
	}
	if err := send(request, &answer); err != nil {
		return "", err
	}
	if answer.UploadURL == "" {
		return "", fmt.Errorf("upload returned no url")
	}
	return answer.UploadURL, nil
}

func (t *AssemblyAITranscriber) queue(ctx context.Context, uploaded string, language string) (string, error) {
	wanted := map[string]any{"audio_url": uploaded, "speech_model": t.model}
	if language != "" {
		wanted["language_code"] = language
	}
	body, err := json.Marshal(wanted)
	if err != nil {
		return "", err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL+"/v2/transcript", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", t.key)
	request.Header.Set("Content-Type", "application/json")

	var answer struct {
		ID string `json:"id"`
	}
	if err := send(request, &answer); err != nil {
		return "", err
	}
	if answer.ID == "" {
		return "", fmt.Errorf("queueing returned no id")
	}
	return answer.ID, nil
}

// polls until the transcript is done, the service gives up on it, or ctx ends
func (t *AssemblyAITranscriber) await(ctx context.Context, id string) (string, error) {
	ticker := time.NewTicker(assemblyAIPoll)
	defer ticker.Stop()

	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, t.baseURL+"/v2/transcript/"+id, nil)
		if err != nil {
			return "", err
		}
		request.Header.Set("Authorization", t.key)

		var answer struct {
			Status string `json:"status"`
			Text   string `json:"text"`
			Error  string `json:"error"`
		}
		if err := send(request, &answer); err != nil {
			return "", err
		}

		switch answer.Status {
		case "completed":
			return answer.Text, nil
		case "error":
			return "", fmt.Errorf("transcript %s failed: %s", id, answer.Error)
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("transcript %s still %s: %w", id, answer.Status, ctx.Err())
		case <-ticker.C:
		}
	}
}
