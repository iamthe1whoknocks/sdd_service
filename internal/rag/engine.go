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
func (e *Engine) Ask(ctx context.Context, question string) (*repository.SearchResult, []repository.SearchResult, error) {
	// Step 1: Generate embedding for the question
	embedding, err := e.ollama.GenerateEmbedding(ctx, question)
	if err != nil {
		return nil, nil, fmt.Errorf("generate question embedding: %w", err)
	}

	// Step 2: Search for similar chunks
	results, err := e.repo.SearchSimilar(ctx, embedding, e.topK)
	if err != nil {
		return nil, nil, fmt.Errorf("search similar chunks: %w", err)
	}

	if len(results) == 0 {
		return nil, nil, fmt.Errorf("no relevant chunks found")
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
		return nil, nil, fmt.Errorf("generate answer: %w", err)
	}

	return &repository.SearchResult{
		Answer: answer,
	}, results, nil
}

// IndexDocument processes a document: chunk it, generate embeddings, and store.
func (e *Engine) IndexDocument(ctx context.Context, title, content string) (string, error) {
	// Step 1: Create document
	docID, err := e.repo.CreateDocument(ctx, title, content)
	if err != nil {
		return "", fmt.Errorf("create document: %w", err)
	}

	// Step 2: Chunk the content
	chunks := e.chunker.Chunk(content)
	if len(chunks) == 0 {
		return docID, nil
	}

	// Step 3: Create chunks in DB
	chunkIDs, err := e.repo.CreateChunks(ctx, docID, chunks)
	if err != nil {
		return "", fmt.Errorf("create chunks: %w", err)
	}

	// Step 4: Generate embeddings for each chunk
	embeddings := make([][]float32, 0, len(chunks))
	for _, chunk := range chunks {
		emb, err := e.ollama.GenerateEmbedding(ctx, chunk)
		if err != nil {
			return "", fmt.Errorf("generate embedding for chunk: %w", err)
		}
		embeddings = append(embeddings, emb)
	}

	// Step 5: Upsert embeddings
	if len(chunkIDs) > 0 {
		if err := e.repo.UpsertEmbeddings(ctx, chunkIDs, embeddings); err != nil {
			return "", fmt.Errorf("upsert embeddings: %w", err)
		}
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
