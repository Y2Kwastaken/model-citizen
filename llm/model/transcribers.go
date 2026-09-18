package model

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// The api a service speaks, named in the models file. Most of them answer
// OpenAI's /audio/transcriptions, which the rotation's shared client already
// handles; the two below are worth the adapter for what their free tiers give.
const (
	apiOpenAI     = "openai"
	apiDeepgram   = "deepgram"
	apiAssemblyAI = "assemblyai"
)

const (
	// an answer is a transcript and some timings, never megabytes
	maxAnswerBytes = 1 << 20
	// how often a queued AssemblyAI transcript is checked on
	pollInterval = 250 * time.Millisecond
)

// Transcriber turns a clip into text. A service that speaks OpenAI's shape
// needs none of this, so transcriberFor returns nil for those and the rotation
// uses its own client.
type Transcriber func(ctx context.Context, clip Clip) (string, error)

func transcriberFor(api string, baseUrl string, authKey string, name string) (Transcriber, error) {
	switch api {
	case "", apiOpenAI:
		return nil, nil
	case apiDeepgram:
		return deepgramTranscriber(baseUrl, authKey, name), nil
	case apiAssemblyAI:
		return assemblyAiTranscriber(baseUrl, authKey, name), nil
	default:
		return nil, fmt.Errorf("unknown api %q, want %q, %q or %q", api, apiOpenAI, apiDeepgram, apiAssemblyAI)
	}
}

// deepgramTranscriber sends the clip as the whole request body and reads the
// transcript out of the first alternative. Deepgram answers in one round trip,
// so there is nothing to wait on.
func deepgramTranscriber(baseUrl string, authKey string, name string) Transcriber {
	return func(ctx context.Context, clip Clip) (string, error) {
		query := url.Values{"model": {name}, "smart_format": {"true"}}
		if clip.Language != "" {
			query.Set("language", clip.Language)
		}

		request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseUrl+"/v1/listen?"+query.Encode(), bytes.NewReader(clip.Data))
		if err != nil {
			return "", err
		}
		request.Header.Set("Authorization", "Token "+authKey)
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
func assemblyAiTranscriber(baseUrl string, authKey string, name string) Transcriber {
	return func(ctx context.Context, clip Clip) (string, error) {
		uploaded, err := assemblyAiUpload(ctx, baseUrl, authKey, clip)
		if err != nil {
			return "", fmt.Errorf("uploading clip: %w", err)
		}

		queued, err := assemblyAiQueue(ctx, baseUrl, authKey, name, uploaded, clip.Language)
		if err != nil {
			return "", fmt.Errorf("queueing clip: %w", err)
		}

		return assemblyAiAwait(ctx, baseUrl, authKey, queued)
	}
}

func assemblyAiUpload(ctx context.Context, baseUrl string, authKey string, clip Clip) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseUrl+"/v2/upload", bytes.NewReader(clip.Data))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", authKey)
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

func assemblyAiQueue(ctx context.Context, baseUrl string, authKey string, name string, uploaded string, language string) (string, error) {
	wanted := map[string]any{"audio_url": uploaded, "speech_model": name}
	if language != "" {
		wanted["language_code"] = language
	}
	body, err := json.Marshal(wanted)
	if err != nil {
		return "", err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseUrl+"/v2/transcript", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", authKey)
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
func assemblyAiAwait(ctx context.Context, baseUrl string, authKey string, id string) (string, error) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseUrl+"/v2/transcript/"+id, nil)
		if err != nil {
			return "", err
		}
		request.Header.Set("Authorization", authKey)

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

// send runs the request and decodes its answer, turning any non-2xx into an
// error so the rotation benches the service rather than reading a body that
// was never a transcript.
func send(request *http.Request, answer any) error {
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxAnswerBytes))
	if err != nil {
		return err
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", response.Status, bytes.TrimSpace(body))
	}
	return json.Unmarshal(body, answer)
}
