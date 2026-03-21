package speech

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewClient(t *testing.T) {
	c := NewClient("sk-test-key")
	if c.apiKey != "sk-test-key" {
		t.Errorf("expected apiKey sk-test-key, got %s", c.apiKey)
	}
	if c.httpClient == nil {
		t.Error("expected httpClient to be initialized")
	}
}

func TestTranscribe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/audio/transcriptions") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Errorf("expected multipart content type, got %s", r.Header.Get("Content-Type"))
		}

		// Verify multipart fields
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if r.FormValue("model") != "whisper-1" {
			t.Errorf("expected model whisper-1, got %s", r.FormValue("model"))
		}
		if r.FormValue("response_format") != "text" {
			t.Errorf("expected response_format text, got %s", r.FormValue("response_format"))
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("expected file field: %v", err)
		}
		defer file.Close()
		if header.Filename != "voice.ogg" {
			t.Errorf("expected filename voice.ogg, got %s", header.Filename)
		}
		data, _ := io.ReadAll(file)
		if string(data) != "fake-audio-data" {
			t.Errorf("unexpected file content: %s", data)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("  Γεια σου κόσμε  \n"))
	}))
	defer srv.Close()

	c := NewClient("test-key")
	c.apiURL = srv.URL // override for test

	text, err := c.Transcribe(context.Background(), []byte("fake-audio-data"), "voice.ogg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "Γεια σου κόσμε" {
		t.Errorf("expected trimmed text, got %q", text)
	}
}

func TestTranscribeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "invalid audio"}`))
	}))
	defer srv.Close()

	c := NewClient("test-key")
	c.apiURL = srv.URL

	_, err := c.Transcribe(context.Background(), []byte("bad-data"), "voice.ogg")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "status 400") {
		t.Errorf("expected status 400 in error, got: %v", err)
	}
}

func TestSynthesize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/audio/speech") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected json content type, got %s", r.Header.Get("Content-Type"))
		}

		body, _ := io.ReadAll(r.Body)
		bodyStr := string(body)
		if !strings.Contains(bodyStr, `"model":"tts-1"`) {
			t.Errorf("expected tts-1 model in body: %s", bodyStr)
		}
		if !strings.Contains(bodyStr, `"voice":"alloy"`) {
			t.Errorf("expected alloy voice in body: %s", bodyStr)
		}
		if !strings.Contains(bodyStr, `"response_format":"opus"`) {
			t.Errorf("expected opus format in body: %s", bodyStr)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake-opus-audio"))
	}))
	defer srv.Close()

	c := NewClient("test-key")
	c.apiURL = srv.URL

	data, err := c.Synthesize(context.Background(), "Hello world", "alloy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "fake-opus-audio" {
		t.Errorf("unexpected audio data: %s", data)
	}
}

func TestSynthesizeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	c := NewClient("test-key")
	c.apiURL = srv.URL

	_, err := c.Synthesize(context.Background(), "Hello", "alloy")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "status 429") {
		t.Errorf("expected status 429 in error, got: %v", err)
	}
}
