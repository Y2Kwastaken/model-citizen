package clients

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

var clip = []byte("RIFF fake wav")

func TestOpenAITranscriber(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/transcriptions" {
			t.Errorf("path %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("model") != "voxtral-mini-latest" || r.FormValue("language") != "en" {
			t.Errorf("model %q language %q", r.FormValue("model"), r.FormValue("language"))
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		if body, _ := io.ReadAll(file); string(body) != string(clip) {
			t.Errorf("uploaded %q", body)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"text":"skip this song"}`)
	}))
	defer server.Close()

	client := openai.NewClient(option.WithBaseURL(server.URL), option.WithAPIKey("key"))
	text, err := NewOpenAITranscriber(client, "voxtral-mini-latest").Transcribe(context.Background(), clip, "en")
	if err != nil || text != "skip this song" {
		t.Fatal(text, err)
	}
}

func TestDeepgramTranscriber(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/listen" || r.URL.Query().Get("model") != "nova-3" || r.URL.Query().Get("language") != "en" {
			t.Errorf("request %s", r.URL)
		}
		if r.Header.Get("Authorization") != "Token key" || r.Header.Get("Content-Type") != "audio/wav" {
			t.Errorf("headers %v", r.Header)
		}
		if body, _ := io.ReadAll(r.Body); string(body) != string(clip) {
			t.Errorf("body %q", body)
		}
		io.WriteString(w, `{"results":{"channels":[{"alternatives":[{"transcript":"skip this song"}]}]}}`)
	}))
	defer server.Close()

	text, err := NewDeepgramTranscriber(server.URL, "key", "nova-3").Transcribe(context.Background(), clip, "en")
	if err != nil || text != "skip this song" {
		t.Fatal(text, err)
	}
}

// silence comes back with no alternatives, which is no text rather than an error
func TestDeepgramSilence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"results":{"channels":[]}}`)
	}))
	defer server.Close()

	text, err := NewDeepgramTranscriber(server.URL, "key", "nova-3").Transcribe(context.Background(), clip, "")
	if err != nil || text != "" {
		t.Fatal(text, err)
	}
}

func TestDeepgramErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key", http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := NewDeepgramTranscriber(server.URL, "key", "nova-3").Transcribe(context.Background(), clip, "")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected a 401 error, got %v", err)
	}
}

// upload, queue, then poll through processing until it completes
func TestAssemblyAITranscriber(t *testing.T) {
	var polls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "key" {
			t.Errorf("authorization %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v2/upload":
			io.WriteString(w, `{"upload_url":"https://cdn/clip"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v2/transcript":
			var queued map[string]string
			json.NewDecoder(r.Body).Decode(&queued)
			if queued["audio_url"] != "https://cdn/clip" || queued["speech_model"] != "universal" || queued["language_code"] != "en" {
				t.Errorf("queued %v", queued)
			}
			io.WriteString(w, `{"id":"abc"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v2/transcript/abc":
			if polls.Add(1) < 3 {
				io.WriteString(w, `{"status":"processing"}`)
				return
			}
			io.WriteString(w, `{"status":"completed","text":"skip this song"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	text, err := NewAssemblyAITranscriber(server.URL, "key", "universal").Transcribe(context.Background(), clip, "en")
	if err != nil || text != "skip this song" {
		t.Fatal(text, err)
	}
	if polls.Load() != 3 {
		t.Fatalf("polled %d times, want 3", polls.Load())
	}
}

func TestAssemblyAIFailedTranscript(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/upload":
			io.WriteString(w, `{"upload_url":"https://cdn/clip"}`)
		case "/v2/transcript":
			io.WriteString(w, `{"id":"abc"}`)
		default:
			io.WriteString(w, `{"status":"error","error":"audio too short"}`)
		}
	}))
	defer server.Close()

	_, err := NewAssemblyAITranscriber(server.URL, "key", "universal").Transcribe(context.Background(), clip, "")
	if err == nil || !strings.Contains(err.Error(), "audio too short") {
		t.Fatalf("expected the service's error, got %v", err)
	}
}
