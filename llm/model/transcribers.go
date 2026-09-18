package model

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// how often a queued AssemblyAI transcript is checked on
const pollInterval = 250 * time.Millisecond

// Transcriber turns a clip into text. A service that speaks OpenAI's shape
// needs none of this, so adaptersFor returns nil for those and the rotation
// uses its own client.
type Transcriber func(ctx context.Context, clip Clip) (string, error)

// deepgramTranscriber sends the clip as the whole request body and reads the
// transcript out of the first alternative. Deepgram answers in one round trip,
// so there is nothing to wait on.
func deepgramTranscriber(service service) Transcriber {
	return func(ctx context.Context, clip Clip) (string, error) {
		query := url.Values{"model": {service.name}, "smart_format": {"true"}}
		if clip.Language != "" {
			query.Set("language", clip.Language)
		}

		request, err := http.NewRequestWithContext(ctx, http.MethodPost, service.baseUrl+"/v1/listen?"+query.Encode(), bytes.NewReader(clip.Data))
		if err != nil {
			return "", err
		}
		request.Header.Set("Authorization", "Token "+service.authKey)
		request.Header.Set("Content-Type", "audio/"+clip.Format)

		var answer struct {
			Results struct {
				Channels []struct {
					Alternatives []struct {
						Transcript string `json:"transcript"`
					} `json:"alternatives"`
				} `json:"channels"`
			} `json:"results"`
		}
		if err := send(request, &answer); err != nil {
			return "", err
		}

		// silence comes back as an empty alternative, or none at all
		if len(answer.Results.Channels) == 0 || len(answer.Results.Channels[0].Alternatives) == 0 {
			return "", nil
		}
		return answer.Results.Channels[0].Alternatives[0].Transcript, nil
	}
}

// assemblyAiTranscriber uploads the clip, queues it, then waits for it.
// AssemblyAI has no synchronous endpoint, so the wait is what its free tier
// costs; the rotation's per-model deadline is what bounds it.
func assemblyAiTranscriber(service service) Transcriber {
	return func(ctx context.Context, clip Clip) (string, error) {
		uploaded, err := assemblyAiUpload(ctx, service, clip)
		if err != nil {
			return "", fmt.Errorf("uploading clip: %w", err)
		}

		queued, err := assemblyAiQueue(ctx, service, uploaded, clip.Language)
		if err != nil {
			return "", fmt.Errorf("queueing clip: %w", err)
		}

		return assemblyAiAwait(ctx, service, queued)
	}
}

func assemblyAiUpload(ctx context.Context, service service, clip Clip) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, service.baseUrl+"/v2/upload", bytes.NewReader(clip.Data))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", service.authKey)
	request.Header.Set("Content-Type", "audio/"+clip.Format)

	var answer struct {
		UploadUrl string `json:"upload_url"`
	}
	if err := send(request, &answer); err != nil {
		return "", err
	}
	if answer.UploadUrl == "" {
		return "", fmt.Errorf("upload returned no url")
	}
	return answer.UploadUrl, nil
}

func assemblyAiQueue(ctx context.Context, service service, uploaded string, language string) (string, error) {
	wanted := map[string]any{"audio_url": uploaded, "speech_model": service.name}
	if language != "" {
		wanted["language_code"] = language
	}
	body, err := json.Marshal(wanted)
	if err != nil {
		return "", err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, service.baseUrl+"/v2/transcript", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", service.authKey)
	request.Header.Set("Content-Type", "application/json")

	var answer struct {
		Id string `json:"id"`
	}
	if err := send(request, &answer); err != nil {
		return "", err
	}
	if answer.Id == "" {
		return "", fmt.Errorf("queueing returned no id")
	}
	return answer.Id, nil
}

// assemblyAiAwait polls the queued transcript until it is done, the service
// gives up on it, or the context runs out.
func assemblyAiAwait(ctx context.Context, service service, id string) (string, error) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, service.baseUrl+"/v2/transcript/"+id, nil)
		if err != nil {
			return "", err
		}
		request.Header.Set("Authorization", service.authKey)

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
