package clients

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var spoken = []byte("ID3 fake mp3")

func TestOpenAISpeaker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path != "/audio/speech" || r.Header.Get("Authorization") != "Bearer key" {
			t.Errorf("request %s %v", r.URL.Path, r.Header)
		}
		if body["model"] != "tts-1" || body["voice"] != "alloy" || body["input"] != "hello" || body["response_format"] != "wav" {
			t.Errorf("body %v", body)
		}
		w.Write(spoken)
	}))
	defer server.Close()

	clip, err := NewOpenAISpeaker(server.URL, "key", "tts-1", "alloy").Speak(context.Background(), "hello")
	if err != nil || string(clip) != string(spoken) {
		t.Fatal(clip, err)
	}
}

func TestMistralSpeaker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["voice"] != "gb_oliver_neutral" || body["model"] != "voxtral-mini-tts" {
			t.Errorf("body %v", body)
		}
		io.WriteString(w, `{"audio_data":"`+base64.StdEncoding.EncodeToString(spoken)+`"}`)
	}))
	defer server.Close()

	clip, err := NewMistralSpeaker(server.URL, "key", "voxtral-mini-tts", "gb_oliver_neutral").Speak(context.Background(), "hello")
	if err != nil || string(clip) != string(spoken) {
		t.Fatal(clip, err)
	}
}

func TestGoogleSpeaker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/text:synthesize" || r.Header.Get("x-goog-api-key") != "key" {
			t.Errorf("request %s %v", r.URL.Path, r.Header)
		}
		var body struct {
			Voice map[string]string `json:"voice"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Voice["languageCode"] != "en-GB" || body.Voice["name"] != "en-GB-Chirp3-HD-Charon" {
			t.Errorf("voice %v", body.Voice)
		}
		io.WriteString(w, `{"audioContent":"`+base64.StdEncoding.EncodeToString(spoken)+`"}`)
	}))
	defer server.Close()

	clip, err := NewGoogleSpeaker(server.URL, "key", "en-GB-Chirp3-HD-Charon").Speak(context.Background(), "hello")
	if err != nil || string(clip) != string(spoken) {
		t.Fatal(clip, err)
	}
}

// a 200 with nothing in it is a failure, not silence
func TestSpeakerEmptyAudio(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"audioContent":""}`)
	}))
	defer server.Close()

	_, err := NewGoogleSpeaker(server.URL, "key", "en-GB-Chirp3-HD-Charon").Speak(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "no audio") {
		t.Fatalf("expected a no audio error, got %v", err)
	}
}

func TestGoogleLanguage(t *testing.T) {
	if language, ok := GoogleLanguage("en-US-Chirp3-HD-Puck"); !ok || language != "en-US" {
		t.Fatal(language, ok)
	}
	if _, ok := GoogleLanguage("Puck"); ok {
		t.Fatal("Puck isn't a full google voice name")
	}
}
