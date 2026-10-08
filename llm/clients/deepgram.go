package clients

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
)

// Deepgram takes the audio as the whole request body and answers in one round trip
type DeepgramTranscriber struct {
	baseURL string
	key     string
	model   string
}

func NewDeepgramTranscriber(baseURL string, key string, model string) *DeepgramTranscriber {
	return &DeepgramTranscriber{baseURL: baseURL, key: key, model: model}
}

func (t *DeepgramTranscriber) Model() string {
	return t.model
}

func (t *DeepgramTranscriber) Transcribe(ctx context.Context, wav []byte, language string) (string, error) {
	query := url.Values{"model": {t.model}, "smart_format": {"true"}}
	if language != "" {
		query.Set("language", language)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL+"/v1/listen?"+query.Encode(), bytes.NewReader(wav))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Token "+t.key)
	request.Header.Set("Content-Type", "audio/wav")

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

	// silence comes back as an empty alternative or none at all
	channels := answer.Results.Channels
	if len(channels) == 0 || len(channels[0].Alternatives) == 0 {
		return "", nil
	}
	return channels[0].Alternatives[0].Transcript, nil
}
