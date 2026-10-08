package clients

import (
	"bytes"
	"context"

	"github.com/openai/openai-go/v3"
)

// turns a clip of speech into text
type Transcriber interface {
	// the model, for logs
	Model() string
	// wav is 16 kHz mono, an empty language lets the service guess
	Transcribe(ctx context.Context, wav []byte, language string) (string, error)
}

// any service with OpenAI's /audio/transcriptions, e.g. Mistral's Voxtral or Groq's Whisper
type OpenAITranscriber struct {
	client openai.Client
	model  string
}

func NewOpenAITranscriber(client openai.Client, model string) *OpenAITranscriber {
	return &OpenAITranscriber{client: client, model: model}
}

func (t *OpenAITranscriber) Model() string {
	return t.model
}

func (t *OpenAITranscriber) Transcribe(ctx context.Context, wav []byte, language string) (string, error) {
	params := openai.AudioTranscriptionNewParams{
		File:  openai.File(bytes.NewReader(wav), "speech.wav", "audio/wav"),
		Model: openai.AudioModel(t.model),
	}
	if language != "" {
		params.Language = openai.String(language)
	}

	transcription, err := t.client.Audio.Transcriptions.New(ctx, params)
	if err != nil {
		return "", err
	}
	return transcription.Text, nil
}
