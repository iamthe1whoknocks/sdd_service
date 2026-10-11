package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SearchResult represents a chunk found by vector similarity search.
type SearchResult struct {
	ChunkID    int64   `json:"chunk_id"`
	DocumentID string  `json:"document_id"`
	Title      string  `json:"title"`
	Content    string  `json:"content"`
	Position   int     `json:"position"`
	Score      float64 `json:"score"`
	Answer     string  `json:"answer,omitempty"`
}

// DB provides database operations for documents, chunks, and embeddings.
type DB struct {
	pool *pgxpool.Pool
}

// New creates a new DB instance.
func New(pool *pgxpool.Pool) *DB {
	return &DB{pool: pool}
}

// CreateDocument inserts a new document and returns its UUID.
func (r *DB) CreateDocument(ctx context.Context, title, content string) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx,
		"INSERT INTO documents (title, content) VALUES ($1, $2) RETURNING id",
		title, content,
	).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

// CreateChunks inserts chunks for a document and returns their IDs.
func (r *DB) CreateChunks(ctx context.Context, docID string, chunks []string) ([]int64, error) {
	if len(chunks) == 0 {
		return nil, nil
	}

	batch := &pgx.Batch{}
	for i := range chunks {
		batch.Queue(
			"INSERT INTO chunks (document_id, content, position) VALUES ($1, $2, $3) RETURNING id",
			docID, chunks[i], i,
		)
	}

	br := r.pool.SendBatch(ctx, batch)
	defer br.Close()

	ids := make([]int64, 0, len(chunks))
	for range chunks {
		var id int64
		if err := br.QueryRow().Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// UpsertEmbeddings inserts or updates embeddings for chunks.
func (r *DB) UpsertEmbeddings(ctx context.Context, chunkIDs []int64, embeddings [][]float32) error {
	if len(chunkIDs) != len(embeddings) {
		return fmt.Errorf("chunkIDs and embeddings length mismatch")
	}

	batch := &pgx.Batch{}
	for i := range chunkIDs {
		batch.Queue(
			"INSERT INTO embeddings (chunk_id, embedding) VALUES ($1, $2) ON CONFLICT (chunk_id) DO UPDATE SET embedding = EXCLUDED.embedding",
			chunkIDs[i], embeddings[i],
		)
	}

	br := r.pool.SendBatch(ctx, batch)
	defer br.Close()

	for range chunkIDs {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

// SearchSimilar retrieves top-k similar chunks based on a query embedding.
func (r *DB) SearchSimilar(ctx context.Context, queryEmbedding []float32, limit int) ([]SearchResult, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT e.chunk_id, c.document_id, d.title, c.content, c.position,
			1 - (e.embedding <=> $1::vector) AS score
		FROM embeddings e
		JOIN chunks c ON e.chunk_id = c.id
		JOIN documents d ON c.document_id = d.id
		ORDER BY e.embedding <=> $1::vector
		LIMIT $2`,
		queryEmbedding, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var sr SearchResult
		if err := rows.Scan(&sr.ChunkID, &sr.DocumentID, &sr.Title, &sr.Content, &sr.Position, &sr.Score); err != nil {
			return nil, err
		}
		results = append(results, sr)
	}
	return results, rows.Err()
}
