package model

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
)

// The shape a service speaks, named in the models file. Most of them answer
// OpenAI's, which the rotation's own client handles; the rest are worth the
// adapter for what their free tiers give.
const (
	apiOpenAI     = "openai"
	apiMistral    = "mistral"
	apiDeepgram   = "deepgram"
	apiAssemblyAI = "assemblyai"
)

// an answer is a transcript or a short clip, never megabytes
const maxAnswerBytes = 8 << 20

// service is what the models file says about one service, with its key
// already resolved.
type service struct {
	baseUrl string
	authKey string
	name    string
	voice   string
}

// api is what one service's shape can do. Transcription has no constructor for
// OpenAI's shape because the rotation's client already speaks it; speech does,
// because a Speaker is how every voice is asked.
type api struct {
	transcribe func(service) Transcriber
	speak      func(service) Speaker
}

// apis is the whole roster of shapes. Teaching the bot a new service is an
// entry here plus the function it names.
var apis = map[string]api{
	apiOpenAI:     {speak: openAiSpeaker},
	apiMistral:    {speak: mistralSpeaker},
	apiDeepgram:   {transcribe: deepgramTranscriber},
	apiAssemblyAI: {transcribe: assemblyAiTranscriber},
}

// adaptersFor resolves how a service is asked for the feature it is listed
// under. Both being nil means the shared client does the asking.
//
// Checking the feature here is what keeps a roster honest: a service that only
// transcribes cannot be listed among the chat models, and the load says so
// rather than every request failing later.
func adaptersFor(feature ModelFeature, apiName string, service service) (Transcriber, Speaker, error) {
	if apiName == "" {
		apiName = apiOpenAI
	}

	shape, known := apis[apiName]
	if !known {
		return nil, nil, fmt.Errorf("unknown api %q, want one of %s", apiName, knownApis())
	}

	switch feature {
	case STT:
		if shape.transcribe != nil {
			return shape.transcribe(service), nil, nil
		}
		if apiName != apiOpenAI {
			return nil, nil, fmt.Errorf("%s does not transcribe", apiName)
		}
		return nil, nil, nil
	case TTS:
		if shape.speak == nil {
			return nil, nil, fmt.Errorf("%s does not speak", apiName)
		}
		return nil, shape.speak(service), nil
	default:
		if apiName != apiOpenAI {
			return nil, nil, fmt.Errorf("%s is not a chat api", apiName)
		}
		return nil, nil, nil
	}
}

func knownApis() string {
	names := make([]string, 0, len(apis))
	for name := range apis {
		names = append(names, name)
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

// fetch runs the request and returns its body, turning any non-2xx into an
// error so the rotation benches the service rather than reading a body that
// was never an answer.
func fetch(request *http.Request) ([]byte, error) {
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxAnswerBytes))
	if err != nil {
		return nil, err
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: %s", response.Status, bytes.TrimSpace(body))
	}
	return body, nil
}

// send is fetch for a service that answers in JSON.
func send(request *http.Request, answer any) error {
	body, err := fetch(request)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, answer)
}
