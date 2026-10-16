package rag

import (
	"context"
	"fmt"
	"strings"

	"vulnrag/internal/repository"
)

// Engine orchestrates the RAG pipeline: retrieve → prompt → generate.
type Engine struct {
	repo      *repository.DB
	ollama    *OllamaClient
	chunker   *Chunker
	topK      int
	promptTpl string
}

// NewEngine creates a new RAG engine.
func NewEngine(repo *repository.DB, ollama *OllamaClient) *Engine {
	return &Engine{
		repo:     repo,
		ollama:   ollama,
		chunker:  NewChunker(),
		topK:     5,
		promptTpl: buildPromptTemplate(),
	}
}

// Ask processes a question through the RAG pipeline.
func (e *Engine) Ask(ctx context.Context, question string) (string, []repository.SearchResult, error) {
	// Step 1: Generate embedding for the question
	embedding, err := e.ollama.GenerateEmbedding(ctx, question)
	if err != nil {
		return "", nil, fmt.Errorf("generate question embedding: %w", err)
	}

	// Step 2: Search for similar chunks
	results, err := e.repo.SearchSimilar(ctx, embedding, e.topK)
	if err != nil {
		return "", nil, fmt.Errorf("search similar chunks: %w", err)
	}

	if len(results) == 0 {
		return "", nil, fmt.Errorf("no relevant chunks found")
	}

	// Step 3: Build context from results
	var contextParts []string
	for _, r := range results {
		contextParts = append(contextParts, fmt.Sprintf("[Source: %s] %s", r.Title, r.Content))
	}
	context := strings.Join(contextParts, "\n\n")

	// Step 4: Build prompt
	prompt := buildPrompt(e.promptTpl, context, question)

	// Step 5: Generate answer
	answer, err := e.ollama.GenerateAnswer(ctx, prompt)
	if err != nil {
		return "", nil, fmt.Errorf("generate answer: %w", err)
	}

	return answer, results, nil
}

// IndexDocument processes a document: chunk it, generate embeddings, and store.
func (e *Engine) IndexDocument(ctx context.Context, title, content string) (string, error) {
	chunks := e.chunker.Chunk(content)

	// Step 1: generate all embeddings first — fallible external calls,
	// no DB state is touched yet, so a failure leaves nothing behind.
	embeddings := make([][]float32, 0, len(chunks))
	for _, chunk := range chunks {
		emb, err := e.ollama.GenerateEmbedding(ctx, chunk)
		if err != nil {
			return "", fmt.Errorf("generate embedding for chunk: %w", err)
		}
		embeddings = append(embeddings, emb)
	}

	// Step 2: persist document, chunks and embeddings atomically.
	docID, err := e.repo.CreateDocumentWithChunks(ctx, title, content, chunks, embeddings)
	if err != nil {
		return "", fmt.Errorf("persist document: %w", err)
	}
	return docID, nil
}



func buildPromptTemplate() string {
	return `You are a security analyst assistant. Answer the question based on the provided context about vulnerabilities.

Context:
{{.Context}}

Question: {{.Question}}

Answer the question concisely based on the context. If the context doesn't contain relevant information, say so.
Provide your answer in Russian if the question is in Russian, otherwise in English.`
}

func buildPrompt(template, context, question string) string {
	prompt := strings.ReplaceAll(template, "{{.Context}}", context)
	prompt = strings.ReplaceAll(prompt, "{{.Question}}", question)
	return prompt
}
