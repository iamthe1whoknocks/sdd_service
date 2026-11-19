package handler

import (
	"context"

	"vulnrag/internal/repository"
)

// Asker runs the RAG pipeline for a question and returns the answer and sources.
// Implemented by *rag.Engine; in tests — mocked.
type Asker interface {
	Ask(ctx context.Context, question string) (string, []repository.SearchResult, error)
}

// Indexer indexes a document into the RAG store and returns its ID.
// Implemented by *rag.Engine; in tests — mocked.
type Indexer interface {
	IndexDocument(ctx context.Context, title, content string) (string, error)
}