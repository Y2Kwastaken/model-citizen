package model

import (
	"encoding/base64"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAiSpeaker(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "audio/wav")
		io.WriteString(w, "RIFF....WAVE")
	}))
	defer server.Close()

	_, speak, err := adaptersFor(TTS, "", service{baseUrl: server.URL + "/v1", name: "kokoro", voice: "bm_george"})
	if err != nil {
		t.Fatalf("adaptersFor: %v", err)
	}

	clip, err := speak(t.Context(), "I am not folding on this one.")
	if err != nil {
		t.Fatalf("speak: %v", err)
	}
	if string(clip.Data) != "RIFF....WAVE" || clip.Format != "wav" {
		t.Errorf("clip = %q, %q", clip.Data, clip.Format)
	}
	if gotPath != "/v1/audio/speech" {
		t.Errorf("posted to %s", gotPath)
	}
	// NOP resolves to no key, and a header with nothing in it is worse than
	// none at all for a server that checks whether one was sent.
	if gotAuth != "" {
		t.Errorf("sent an authorization header of %q for a keyless service", gotAuth)
	}

	var asked struct {
		Model  string `json:"model"`
		Input  string `json:"input"`
		Voice  string `json:"voice"`
		Format string `json:"response_format"`
	}
	if err := json.Unmarshal(gotBody, &asked); err != nil {
		t.Fatalf("body %s: %v", gotBody, err)
	}
	if asked.Model != "kokoro" || asked.Voice != "bm_george" || asked.Input != "I am not folding on this one." || asked.Format != "wav" {
		t.Errorf("asked for %+v", asked)
	}
}

func TestOpenAiSpeakerSendsAKeyWhenItHasOne(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		io.WriteString(w, "RIFF....WAVE")
	}))
	defer server.Close()

	_, speak, _ := adaptersFor(TTS, apiOpenAI, service{baseUrl: server.URL, authKey: "secret", name: "tts-1", voice: "alloy"})
	if _, err := speak(t.Context(), "hello"); err != nil {
		t.Fatalf("speak: %v", err)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("auth = %q", gotAuth)
	}
}

func TestOpenAiSpeakerFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, "no such voice")
	}))
	defer server.Close()

	_, speak, _ := adaptersFor(TTS, "", service{baseUrl: server.URL, name: "kokoro", voice: "bm_nobody"})
	if _, err := speak(t.Context(), "hello"); err == nil || !strings.Contains(err.Error(), "no such voice") {
		t.Fatalf("err = %v", err)
	}
}

// Mistral takes OpenAI's request and answers with base64 in a JSON body, so
// the clip has to be unwrapped before anything can play it.
func TestMistralSpeaker(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		io.WriteString(w, `{"audio_data":"`+base64.StdEncoding.EncodeToString([]byte("ID3mp3"))+`"}`)
	}))
	defer server.Close()

	_, speak, err := adaptersFor(TTS, apiMistral, service{
		baseUrl: server.URL + "/v1",
		authKey: "secret",
		name:    "voxtral-mini-tts-2603",
		voice:   "gb_oliver_neutral",
	})
	if err != nil {
		t.Fatalf("adaptersFor: %v", err)
	}

	clip, err := speak(t.Context(), "I am not folding on this one.")
	if err != nil {
		t.Fatalf("speak: %v", err)
	}
	if string(clip.Data) != "ID3mp3" || clip.Format != "mp3" {
		t.Errorf("clip = %q, %q", clip.Data, clip.Format)
	}
	if gotPath != "/v1/audio/speech" || gotAuth != "Bearer secret" {
		t.Errorf("posted to %s as %q", gotPath, gotAuth)
	}

	var asked struct {
		Model string `json:"model"`
		Input string `json:"input"`
		Voice string `json:"voice"`
	}
	if err := json.Unmarshal(gotBody, &asked); err != nil {
		t.Fatalf("body %s: %v", gotBody, err)
	}
	if asked.Model != "voxtral-mini-tts-2603" || asked.Voice != "gb_oliver_neutral" {
		t.Errorf("asked for %+v", asked)
	}
}

func TestMistralSpeakerFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"message":"Voice 'nope' not found."}`)
	}))
	defer server.Close()

	_, speak, _ := adaptersFor(TTS, apiMistral, service{baseUrl: server.URL, authKey: "secret", name: "voxtral-mini-tts-2603", voice: "nope"})
	if _, err := speak(t.Context(), "hello"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
}

// The feature a roster is loaded for is what keeps a service out of a file it
// cannot serve -- at load, rather than on every request.
func TestAdaptersForChecksTheFeature(t *testing.T) {
	cases := []struct {
		feature ModelFeature
		api     string
		wantErr bool
	}{
		{TTS, apiOpenAI, false},
		{TTS, "", false},
		{TTS, apiMistral, false},
		{TTS, apiDeepgram, true},
		{TTS, apiAssemblyAI, true},
		{STT, apiDeepgram, false},
		{STT, apiAssemblyAI, false},
		{STT, "", false},
		{Chat, "", false},
		{Chat, apiDeepgram, true},
		{Chat, apiMistral, true},
		{STT, apiMistral, true},
	}

	for _, c := range cases {
		_, _, err := adaptersFor(c.feature, c.api, service{baseUrl: "https://one.test", authKey: "secret", name: "a-name", voice: "a-voice"})
		if (err != nil) != c.wantErr {
			t.Errorf("feature %d with api %q: err = %v, wanted an error: %t", c.feature, c.api, err, c.wantErr)
		}
	}
}

// The roster in data/speech-models.json is the real thing this has to load.
func TestSpeechRosterLoads(t *testing.T) {
	t.Setenv("MISTRAL_KEY", "secret")

	models, err := readModelsFile("../../data/speech-models.json")
	if err != nil {
		t.Fatalf("readModelsFile: %v", err)
	}

	provider, err := newModelProvider(models, TTS)
	if err != nil {
		t.Fatalf("newModelProvider: %v", err)
	}
	if provider.Count() != len(models) {
		t.Fatalf("loaded %d of %d", provider.Count(), len(models))
	}
	for _, loaded := range provider.models {
		if loaded.Speak == nil {
			t.Errorf("%s got no speaker", loaded.Name)
		}
	}
}
