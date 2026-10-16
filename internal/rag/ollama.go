package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// OllamaClient provides methods to interact with Ollama API.
type OllamaClient struct {
	BaseURL    string
	EmbedModel string
	LLMModel   string
	client     *http.Client
}

// NewOllamaClient creates a new Ollama client from environment variables.
func NewOllamaClient() *OllamaClient {
	return &OllamaClient{
		BaseURL:    getEnvDefault("OLLAMA_HOST", "http://localhost:11434"),
		EmbedModel: getEnvDefault("EMBED_MODEL", "paraphrase-multilingual"),
		LLMModel:   getEnvDefault("LLM_MODEL", "qwen2.5:3b"),
		client: &http.Client{
			Timeout: 65 * time.Second, // Slightly more than 60s timeout for ask
		},
	}
}

func getEnvDefault(key, def string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return def
}

// EmbeddingRequest is the request body for the /api/embeddings endpoint.
type EmbeddingRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// EmbeddingResponse is the response from the /api/embeddings endpoint.
type EmbeddingResponse struct {
	Embedding []float32 `json:"embedding"`
}

// GenerateRequest is the request body for the /api/generate endpoint.
type GenerateRequest struct {
	Model   string                 `json:"model"`
	Prompt  string                 `json:"prompt"`
	Stream  bool                   `json:"stream"`
	Format  string                 `json:"format,omitempty"`
	Options map[string]interface{} `json:"options,omitempty"`
}

// GenerateResponse is the streaming response from the /api/generate endpoint.
type GenerateResponse struct {
	Model      string `json:"model"`
	Response   string `json:"response"`
	Done       bool   `json:"done"`
	DoneReason string `json:"done_reason,omitempty"`
}

// GenerateFullResponse accumulates the full response from streaming.
type GenerateFullResponse struct {
	Answer string
}

// GenerateEmbedding creates an embedding vector for the given text.
func (c *OllamaClient) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	reqBody := EmbeddingRequest{
		Model:  c.EmbedModel,
		Prompt: text,
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/embeddings", bytes.NewReader(jsonBytes))
	if err != nil {
		return nil, fmt.Errorf("error creating post request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)

	if err != nil {
		return nil, fmt.Errorf("call ollama embeddings: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embeddings error %d: %s", resp.StatusCode, string(body))
	}

	var embedResp EmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}

	if len(embedResp.Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding returned")
	}

	return embedResp.Embedding, nil
}

// GenerateAnswer generates an answer using the LLM.
func (c *OllamaClient) GenerateAnswer(ctx context.Context, prompt string) (string, error) {
	reqBody := GenerateRequest{
		Model:  c.LLMModel,
		Prompt: prompt,
		Stream: false,
		Options: map[string]interface{}{
			"temperature": 0.7,
			"num_predict": 2048,
		},
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal generate request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/generate", bytes.NewReader(jsonBytes))
	if err != nil {
		return "", fmt.Errorf("error creating post request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)

	if err != nil {
		return "", fmt.Errorf("call ollama generate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama generate error %d: %s", resp.StatusCode, string(body))
	}

	var genResp GenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&genResp); err != nil {
		return "", fmt.Errorf("decode generate response: %w", err)
	}

	return genResp.Response, nil
}
