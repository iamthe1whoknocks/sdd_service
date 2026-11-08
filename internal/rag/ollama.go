package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
	"vulnrag/internal/config"
)

// defaultCallTimeout bounds a single Ollama HTTP call when the caller's
// context carries no deadline of its own (docs/TODO_SIGNIFICANT.md SIG-4).
const defaultCallTimeout = 120 * time.Second

// OllamaClient provides methods to interact with Ollama API.
type OllamaClient struct {
	BaseURL    string
	EmbedModel string
	LLMModel   string
	client     *http.Client
	// callTimeout is the per-call safety cap applied on top of the caller's
	// context by withCallTimeout.
	callTimeout time.Duration
}

// NewOllamaClient creates a new Ollama client from environment variables.
//
// The underlying http.Client deliberately has no fixed Timeout: the effective
// deadline of every request comes from its context.  Each call wraps the
// caller's context with the per-call cap OLLAMA_CALL_TIMEOUT (default 120s,
// see withCallTimeout), so a hung Ollama can never pin a handler goroutine
// forever, while a shorter caller deadline (e.g. the 60s /ask middleware)
// still wins (docs/TODO_SIGNIFICANT.md SIG-4).
func NewOllamaClient() *OllamaClient {
	return &OllamaClient{
		BaseURL:     config.String("OLLAMA_HOST", "http://localhost:11434"),
		EmbedModel:  config.String("EMBED_MODEL", "nomic-embed-text:latest"),
		LLMModel:    config.String("LLM_MODEL", "qwen2.5:3b"),
		client:      &http.Client{},
		callTimeout: config.Duration("OLLAMA_CALL_TIMEOUT", defaultCallTimeout),
	}
}

// withCallTimeout bounds a single Ollama call by the per-call cap.
//
// context.WithTimeout derives a deadline of min(parent deadline, callTimeout),
// so the cap never extends a tighter caller deadline and never outlives the
// parent context.  The returned cancel func must be called to release resources.
//
// A non-positive callTimeout (the zero value of a hand-built OllamaClient, as
// used in tests) falls back to defaultCallTimeout: an unset cap must mean the
// documented default, not an already-expired deadline that fails every call.
func (c *OllamaClient) withCallTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := c.callTimeout
	if timeout <= 0 {
		timeout = defaultCallTimeout
	}
	return context.WithTimeout(ctx, timeout)
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

// BatchEmbeddingRequest is the request body for the /api/embed endpoint.
type BatchEmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// BatchEmbeddingResponse is the response from the /api/embed endpoint.
type BatchEmbeddingResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
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

// GenerateEmbeddings creates embedding vectors for multiple texts in a single
// Ollama call via the /api/embed endpoint (docs/TODO_SIGNIFICANT.md SIG-5).
//
// texts must be non-empty; if len(texts) == 0 the method returns an empty
// slice (no HTTP round-trip).  The per-call cap from withCallTimeout bounds
// the request the same way as GenerateEmbedding does
// (docs/TODO_SIGNIFICANT.md SIG-4).
func (c *OllamaClient) GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	ctx, cancel := c.withCallTimeout(ctx)
	defer cancel()

	reqBody := BatchEmbeddingRequest{
		Model: c.EmbedModel,
		Input: texts,
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal batch embedding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/embed", bytes.NewReader(jsonBytes))
	if err != nil {
		return nil, fmt.Errorf("error creating batch embed request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)

	if err != nil {
		return nil, fmt.Errorf("call ollama batch embeddings: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama batch embeddings error %d: %s", resp.StatusCode, string(body))
	}

	var embedResp BatchEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("decode batch embedding response: %w", err)
	}

	if len(embedResp.Embeddings) == 0 {
		return nil, fmt.Errorf("empty batch embedding returned")
	}

	return embedResp.Embeddings, nil
}

// GenerateEmbedding creates an embedding vector for the given text.
func (c *OllamaClient) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	ctx, cancel := c.withCallTimeout(ctx)
	defer cancel()

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
	ctx, cancel := c.withCallTimeout(ctx)
	defer cancel()

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
