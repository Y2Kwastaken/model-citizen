package clients

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// turns text into a clip of it being said, any format ffmpeg can read
type Speaker interface {
	// the voice, for logs
	Voice() string
	Speak(ctx context.Context, text string) ([]byte, error)
}

// any service with OpenAI's /audio/speech, the answer is the audio itself
type OpenAISpeaker struct {
	baseURL string
	key     string
	model   string
	voice   string
}

func NewOpenAISpeaker(baseURL string, key string, model string, voice string) *OpenAISpeaker {
	return &OpenAISpeaker{baseURL: baseURL, key: key, model: model, voice: voice}
}

func (s *OpenAISpeaker) Voice() string {
	return s.voice
}

func (s *OpenAISpeaker) Speak(ctx context.Context, text string) ([]byte, error) {
	body, err := json.Marshal(map[string]string{
		"model":           s.model,
		"input":           text,
		"voice":           s.voice,
		"response_format": "wav",
	})
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/audio/speech", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+s.key)

	clip, err := fetch(request)
	if err != nil {
		return nil, err
	}
	if len(clip) == 0 {
		return nil, fmt.Errorf("%s returned no audio", s.voice)
	}
	return clip, nil
}

// Mistral's Voxtral takes OpenAI's request but answers with base64 mp3 in json
type MistralSpeaker struct {
	baseURL string
	key     string
	model   string
	voice   string
}

func NewMistralSpeaker(baseURL string, key string, model string, voice string) *MistralSpeaker {
	return &MistralSpeaker{baseURL: baseURL, key: key, model: model, voice: voice}
}

func (s *MistralSpeaker) Voice() string {
	return s.voice
}

func (s *MistralSpeaker) Speak(ctx context.Context, text string) ([]byte, error) {
	body, err := json.Marshal(map[string]string{
		"model": s.model,
		"input": text,
		"voice": s.voice,
	})
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/audio/speech", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+s.key)

	var answer struct {
		AudioData string `json:"audio_data"`
	}
	if err := send(request, &answer); err != nil {
		return nil, err
	}
	return decodeClip(answer.AudioData, s.voice)
}

// Google Cloud Text-to-Speech has no model apart from the voice, the voice
// name says both, e.g. en-GB-Chirp3-HD-Charon
type GoogleSpeaker struct {
	baseURL string
	key     string
	voice   string
}

func NewGoogleSpeaker(baseURL string, key string, voice string) *GoogleSpeaker {
	return &GoogleSpeaker{baseURL: baseURL, key: key, voice: voice}
}

func (s *GoogleSpeaker) Voice() string {
	return s.voice
}

func (s *GoogleSpeaker) Speak(ctx context.Context, text string) ([]byte, error) {
	language, ok := GoogleLanguage(s.voice)
	if !ok {
		return nil, fmt.Errorf("%q is not a google voice name", s.voice)
	}

	body, err := json.Marshal(map[string]any{
		"input":       map[string]string{"text": text},
		"voice":       map[string]string{"languageCode": language, "name": s.voice},
		"audioConfig": map[string]string{"audioEncoding": "MP3"},
	})
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/text:synthesize", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("x-goog-api-key", s.key)

	var answer struct {
		AudioContent string `json:"audioContent"`
	}
	if err := send(request, &answer); err != nil {
		return nil, err
	}
	return decodeClip(answer.AudioContent, s.voice)
}

// the language a google voice name starts with, en-GB of en-GB-Chirp3-HD-Charon
func GoogleLanguage(voice string) (string, bool) {
	parts := strings.SplitN(voice, "-", 3)
	if len(parts) < 3 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	return parts[0] + "-" + parts[1], true
}

func decodeClip(encoded string, voice string) ([]byte, error) {
	clip, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decoding the clip: %w", err)
	}
	if len(clip) == 0 {
		return nil, fmt.Errorf("%s returned no audio", voice)
	}
	return clip, nil
}
