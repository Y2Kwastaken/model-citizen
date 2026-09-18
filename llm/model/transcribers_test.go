package model

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testClip = Clip{Data: []byte("RIFF....WAVEfmt "), Format: "wav", Language: "en"}

func TestDeepgramTranscriber(t *testing.T) {
	var gotQuery, gotAuth, gotType string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/listen" {
			t.Errorf("path = %q", r.URL.Path)
		}
		gotQuery, gotAuth, gotType = r.URL.RawQuery, r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		io.WriteString(w, `{"results":{"channels":[{"alternatives":[{"transcript":"hey model"}]}]}}`)
	}))
	defer server.Close()

	transcribe, err := transcriberFor(apiDeepgram, server.URL, "secret", "nova-3")
	if err != nil {
		t.Fatalf("transcriberFor: %v", err)
	}

	text, err := transcribe(t.Context(), testClip)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if text != "hey model" {
		t.Errorf("text = %q", text)
	}
	if gotAuth != "Token secret" {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotType != "audio/wav" {
		t.Errorf("content type = %q", gotType)
	}
	if string(gotBody) != string(testClip.Data) {
		t.Errorf("body = %q", gotBody)
	}
	for _, want := range []string{"model=nova-3", "language=en", "smart_format=true"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q is missing %q", gotQuery, want)
		}
	}
}

// Deepgram reports silence as a channel with nothing in it, which is not a
// failure -- the caller drops empty lines.
func TestDeepgramTranscriberHeardNothing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"results":{"channels":[]}}`)
	}))
	defer server.Close()

	transcribe, _ := transcriberFor(apiDeepgram, server.URL, "secret", "nova-3")
	text, err := transcribe(t.Context(), testClip)
	if err != nil || text != "" {
		t.Fatalf("got %q, %v", text, err)
	}
}

func TestAssemblyAiTranscriber(t *testing.T) {
	var uploaded, queued string
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "secret" {
			t.Errorf("auth = %q", got)
		}
		switch {
		case r.URL.Path == "/v2/upload":
			body, _ := io.ReadAll(r.Body)
			uploaded = string(body)
			io.WriteString(w, `{"upload_url":"https://cdn.test/clip"}`)
		case r.URL.Path == "/v2/transcript" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			queued = string(body)
			io.WriteString(w, `{"id":"t-1","status":"queued"}`)
		case r.URL.Path == "/v2/transcript/t-1":
			// the first look is always too early, that is the point of polling
			polls++
			if polls < 2 {
				io.WriteString(w, `{"status":"processing"}`)
				return
			}
			io.WriteString(w, `{"status":"completed","text":"hey model"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	transcribe, err := transcriberFor(apiAssemblyAI, server.URL, "secret", "universal")
	if err != nil {
		t.Fatalf("transcriberFor: %v", err)
	}

	text, err := transcribe(t.Context(), testClip)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if text != "hey model" {
		t.Errorf("text = %q", text)
	}
	if uploaded != string(testClip.Data) {
		t.Errorf("uploaded %q", uploaded)
	}
	for _, want := range []string{`"audio_url":"https://cdn.test/clip"`, `"speech_model":"universal"`, `"language_code":"en"`} {
		if !strings.Contains(queued, want) {
			t.Errorf("queued %s is missing %s", queued, want)
		}
	}
	if polls != 2 {
		t.Errorf("polled %d times", polls)
	}
}

func TestAssemblyAiTranscriberFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/upload":
			io.WriteString(w, `{"upload_url":"https://cdn.test/clip"}`)
		case "/v2/transcript":
			io.WriteString(w, `{"id":"t-1"}`)
		default:
			io.WriteString(w, `{"status":"error","error":"audio too short"}`)
		}
	}))
	defer server.Close()

	transcribe, _ := transcriberFor(apiAssemblyAI, server.URL, "secret", "universal")
	_, err := transcribe(t.Context(), testClip)
	if err == nil || !strings.Contains(err.Error(), "audio too short") {
		t.Fatalf("err = %v", err)
	}
}

// A service that refuses the clip has to error, or the rotation would take an
// error page for a transcript and never bench it.
func TestTranscriberRejectsBadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"err_msg":"bad credentials"}`)
	}))
	defer server.Close()

	for _, api := range []string{apiDeepgram, apiAssemblyAI} {
		transcribe, _ := transcriberFor(api, server.URL, "secret", "whatever")
		if _, err := transcribe(t.Context(), testClip); err == nil {
			t.Errorf("%s: no error", api)
		} else if !strings.Contains(err.Error(), "bad credentials") {
			t.Errorf("%s: err = %v", api, err)
		}
	}
}

// A queue that never finishes must end with the caller's deadline, not hang.
func TestAssemblyAiTranscriberGivesUp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/upload":
			io.WriteString(w, `{"upload_url":"https://cdn.test/clip"}`)
		case "/v2/transcript":
			io.WriteString(w, `{"id":"t-1"}`)
		default:
			io.WriteString(w, `{"status":"processing"}`)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 600*time.Millisecond)
	defer cancel()

	transcribe, _ := transcriberFor(apiAssemblyAI, server.URL, "secret", "universal")
	if _, err := transcribe(ctx, testClip); err == nil {
		t.Fatal("waited forever")
	}
}

func TestTranscriberForOpenAiAndUnknown(t *testing.T) {
	for _, api := range []string{"", apiOpenAI} {
		transcribe, err := transcriberFor(api, "https://one.test/v1", "secret", "whisper-1")
		if err != nil || transcribe != nil {
			t.Errorf("api %q: got %v, %v, want the rotation's own client", api, transcribe, err)
		}
	}

	if _, err := transcriberFor("depgram", "https://one.test", "secret", "nova-3"); err == nil {
		t.Error("a typo'd api loaded anyway")
	}
}

// The roster in data/voice-models.json is the real thing this has to load.
func TestNewModelProviderAttachesTranscribers(t *testing.T) {
	t.Setenv("DEEPGRAM_KEY", "one")
	t.Setenv("MISTRAL_KEY", "two")
	t.Setenv("ASSEMBLYAI_KEY", "")

	models, err := readModelsFile(filepath.Join("..", "..", "data", "voice-models.json"))
	if err != nil {
		t.Fatalf("readModelsFile: %v", err)
	}

	provider, err := newModelProvider(models)
	if err != nil {
		t.Fatalf("newModelProvider: %v", err)
	}

	// the keyless service is dropped, the rest keep their order
	if len(provider.models) != 2 {
		t.Fatalf("loaded %d models: %+v", len(provider.models), provider.models)
	}
	if provider.models[0].Transcribe == nil {
		t.Errorf("%s speaks its own api and got no transcriber", provider.models[0].Name)
	}
	if provider.models[1].Transcribe != nil {
		t.Errorf("%s speaks OpenAI's api and should use the client", provider.models[1].Name)
	}
}
