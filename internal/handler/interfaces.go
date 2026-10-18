package handler

import (
	"context"

	"vulnrag/internal/repository"
)

// Asker запускает RAG-конвейер для вопроса и возвращает ответ и источники.
// Реализуется *rag.Engine; в тестах — фейком.
type Asker interface {
	Ask(ctx context.Context, question string) (string, []repository.SearchResult, error)
}

// Indexer индексирует документ в RAG-хранилище и возвращает его ID.
// Реализуется *rag.Engine; в тестах — фейком.
type Indexer interface {
	IndexDocument(ctx context.Context, title, content string) (string, error)
}