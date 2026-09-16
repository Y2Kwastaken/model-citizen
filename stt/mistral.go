// Package stt is a proof-of-concept speech-to-text client.
//
// It exists to answer one question -- is cloud transcription of Discord voice
// accurate enough and fast enough to build on -- so it deliberately has none of
// the machinery a real one would want: no provider rotation, no failover, no
// retries. llm/model_selector.go is the shape those would take when this stops
// being an experiment.
package stt

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"time"
)

const (
	// Voxtral Mini Transcribe 2, Mistral's transcription-only model. Their
	// endpoint mirrors OpenAI's, so moving to Groq later is a URL and a model
	// name rather than a rewrite.
	defaultModel    = "voxtral-mini-latest"
	defaultEndpoint = "https://api.mistral.ai/v1/audio/transcriptions"

	// Generous for a clip of a few seconds. The caller is expected to impose
	// something tighter through the context; this only stops a hung connection
	// leaking a goroutine.
	requestTimeout = 30 * time.Second

	// A transcript is small. Anything larger is an error page from something
	// that is not the API, and does not need reading in full to be reported.
	maxResponseBytes = 1 << 20
)

// Client transcribes audio through Mistral's offline transcription endpoint.
type Client struct {
	http     *http.Client
	endpoint string
	key      string
	model    string
}

// NewMistral builds a client from the API key held in the named environment
// variable.
//
// The returned client is safe for concurrent use and should be kept: its
// connection pool is the point. Mistral is EU-hosted, so from a German or
// Finnish box the round trip is ~20ms, and a fresh TLS handshake on every
// transcription would be a visible share of it.
func NewMistral(keyVariable string) (*Client, error) {
	key := os.Getenv(keyVariable)
	if key == "" {
		return nil, fmt.Errorf("key not set in environment at variable %s", keyVariable)
	}

	return &Client{
		http:     &http.Client{Timeout: requestTimeout},
		endpoint: defaultEndpoint,
		key:      key,
		model:    defaultModel,
	}, nil
}

// Transcribe sends wav to Mistral and returns the recognised text.
//
// language is an ISO-639-1 code. Passing one skips detection, which is worth a
// little latency and some accuracy on short clips; passing "" lets the model
// decide.
func (c *Client) Transcribe(ctx context.Context, wav []byte, language string) (string, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)

	fields := map[string]string{"model": c.model}
	if language != "" {
		fields["language"] = language
	}
	for name, value := range fields {
		if err := form.WriteField(name, value); err != nil {
			return "", fmt.Errorf("writing %s field: %w", name, err)
		}
	}

	// The extension is how the API sniffs the format, so it has to match what
	// the bytes actually are.
	file, err := form.CreateFormFile("file", "audio.wav")
	if err != nil {
		return "", fmt.Errorf("creating file field: %w", err)
	}
	if _, err := file.Write(wav); err != nil {
		return "", fmt.Errorf("writing audio: %w", err)
	}
	if err := form.Close(); err != nil {
		return "", fmt.Errorf("closing form: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, &body)
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", form.FormDataContentType())

	res, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("calling %s: %w", c.endpoint, err)
	}
	defer res.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("transcription failed: %s: %s", res.Status, bytes.TrimSpace(payload))
	}

	var decoded struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	return decoded.Text, nil
}
