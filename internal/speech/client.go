package speech

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

const openAIURL = "https://api.openai.com/v1"

// Client wraps the OpenAI speech API for transcription (STT) and synthesis (TTS).
type Client struct {
	apiURL     string
	apiKey     string
	httpClient *http.Client
}

// NewClient creates an OpenAI speech API client.
func NewClient(apiKey string) *Client {
	return &Client{
		apiURL: openAIURL,
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Transcribe sends audio data to the OpenAI Whisper endpoint and returns
// the transcribed text. Language is auto-detected.
func (c *Client) Transcribe(ctx context.Context, audio []byte, filename string) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(audio); err != nil {
		return "", fmt.Errorf("write audio data: %w", err)
	}

	_ = writer.WriteField("model", "whisper-1")
	_ = writer.WriteField("response_format", "text")

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+"/audio/transcriptions", &body)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("transcription request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("transcription failed (status %d): %s", resp.StatusCode, errBody)
	}

	text, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	return strings.TrimSpace(string(text)), nil
}

// ttsRequest is the JSON body for the text-to-speech endpoint.
type ttsRequest struct {
	Model          string `json:"model"`
	Voice          string `json:"voice"`
	Input          string `json:"input"`
	ResponseFormat string `json:"response_format"`
}

// Synthesize converts text to speech audio (OGG/Opus format) via OpenAI TTS.
func (c *Client) Synthesize(ctx context.Context, text, voice string) ([]byte, error) {
	reqBody := ttsRequest{
		Model:          "tts-1",
		Voice:          voice,
		Input:          text,
		ResponseFormat: "opus",
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+"/audio/speech", bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tts request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("tts failed (status %d): %s", resp.StatusCode, errBody)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read tts response: %w", err)
	}

	return data, nil
}
