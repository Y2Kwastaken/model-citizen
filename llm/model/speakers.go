package model

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"net/http"
)

// Speaker turns text into a clip of the bot saying it.
type Speaker func(ctx context.Context, text string) (Clip, error)

// openAiSpeaker asks for speech the way OpenAI's /audio/speech does: the model
// and the voice are separate, and the answer is the audio itself rather than
// anything wrapped around it.
//
// Every hosted voice worth using speaks this, and so does a local one behind a
// wrapper, which is why the bot's own voice needs no adapter of its own.
func openAiSpeaker(service service) Speaker {
	return func(ctx context.Context, text string) (Clip, error) {
		body, err := json.Marshal(map[string]any{
			"model": service.name,
			"input": text,
			"voice": service.voice,
			// wav costs bandwidth we are not paying for over localhost, and
			// saves ffmpeg guessing at a container.
			"response_format": "wav",
		})
		if err != nil {
			return Clip{}, err
		}

		request, err := http.NewRequestWithContext(ctx, http.MethodPost, service.baseUrl+"/audio/speech", bytes.NewReader(body))
		if err != nil {
			return Clip{}, err
		}
		request.Header.Set("Content-Type", "application/json")
		if service.authKey != "" {
			request.Header.Set("Authorization", "Bearer "+service.authKey)
		}

		wav, err := fetch(request)
		if err != nil {
			return Clip{}, err
		}
		if len(wav) == 0 {
			return Clip{}, fmt.Errorf("%s returned no audio", service.voice)
		}

		return Clip{Data: wav, Format: "wav"}, nil
	}
}

// mistralSpeaker asks Mistral for speech. The request is OpenAI's, but the
// answer is base64 inside a JSON body rather than the audio itself, which is
// the whole reason this is not openAiSpeaker.
func mistralSpeaker(service service) Speaker {
	return func(ctx context.Context, text string) (Clip, error) {
		body, err := json.Marshal(map[string]any{
			"model": service.name,
			"input": text,
			"voice": service.voice,
		})
		if err != nil {
			return Clip{}, err
		}

		request, err := http.NewRequestWithContext(ctx, http.MethodPost, service.baseUrl+"/audio/speech", bytes.NewReader(body))
		if err != nil {
			return Clip{}, err
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+service.authKey)

		var answer struct {
			AudioData string `json:"audio_data"`
		}
		if err := send(request, &answer); err != nil {
			return Clip{}, err
		}

		audio, err := base64.StdEncoding.DecodeString(answer.AudioData)
		if err != nil {
			return Clip{}, fmt.Errorf("decoding the clip: %w", err)
		}
		if len(audio) == 0 {
			return Clip{}, fmt.Errorf("%s returned no audio", service.voice)
		}

		return Clip{Data: audio, Format: "mp3"}, nil
	}
}
