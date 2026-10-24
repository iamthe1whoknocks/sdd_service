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

// UpsertDocument inserts a document with the given title or updates it if a
// document with the same title already exists.  Returns (docID, true) on
// insert and (existingID, false) on update.  This enables idempotent vullist
// indexing (docs/FIXES.md #7).
func (r *DB) UpsertDocument(ctx context.Context, title, content string) (docID string, created bool, err error) {
	err = r.pool.QueryRow(ctx,
		`INSERT INTO documents (title, content) VALUES ($1, $2)
		 ON CONFLICT (title) DO UPDATE SET content = EXCLUDED.content
		 RETURNING id`,
		title, content,
	).Scan(&docID)
	if err != nil {
		return "", false, err
	}
	created = true
	return docID, true, nil
}




// SearchSimilar retrieves up to limit chunks whose cosine similarity to
// queryEmbedding is at least minScore.
//
// A plain k-NN query always returns `limit` rows, even when the whole
// knowledge base is irrelevant. Filtering by minScore keeps noise out of the
// prompt (docs/FIXES.md #9). The filter runs after ordering so the scan stays
// a plain index-backed k-NN query.
func (r *DB) SearchSimilar(ctx context.Context, queryEmbedding []float32, limit int, minScore float64) ([]SearchResult, error) {
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
		// score = 1 - cosine_distance; drop chunks below the floor.
		if sr.Score < minScore {
			continue
		}
		results = append(results, sr)
	}
	return results, rows.Err()
}


// CreateDocumentWithChunks atomically inserts a document, its chunks and
// embeddings in a single transaction.
func (r *DB) CreateDocumentWithChunks(ctx context.Context, title, content string, chunks []string, embeddings [][]float32) (string, error) {
	if len(chunks) != len(embeddings) {
		return "", fmt.Errorf("chunks and embeddings length mismatch")
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx) // no-op after successful Commit

	var docID string
	if err := tx.QueryRow(ctx,
		"INSERT INTO documents (title, content) VALUES ($1, $2) RETURNING id",
		title, content,
	).Scan(&docID); err != nil {
		return "", err
	}

	// Chunks в батче внутри транзакции
	batch := &pgx.Batch{}
	for i, ch := range chunks {
		batch.Queue(
			"INSERT INTO chunks (document_id, content, position) VALUES ($1, $2, $3) RETURNING id",
			docID, ch, i,
		)
	}
	br := tx.SendBatch(ctx, batch)
	chunkIDs := make([]int64, 0, len(chunks))
	for range chunks {
		var id int64
		if err := br.QueryRow().Scan(&id); err != nil {
			br.Close()
			return "", err
		}
		chunkIDs = append(chunkIDs, id)
	}
	if err := br.Close(); err != nil {
		return "", err
	}

	for i, id := range chunkIDs {
		if _, err := tx.Exec(ctx,
			"INSERT INTO embeddings (chunk_id, embedding) VALUES ($1, $2)",
			id, embeddings[i],
		); err != nil {
			return "", err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return docID, nil
}

// UpsertDocumentWithChunks atomically upserts a document and replaces its
// chunks and embeddings.  If a document with the same title already exists,
// old chunks/embeddings are deleted and new ones are inserted.
// Returns (docID, true) on insert, (docID, false) on update.
func (r *DB) UpsertDocumentWithChunks(ctx context.Context, title, content string, chunks []string, embeddings [][]float32) (string, bool, error) {
	if len(chunks) != len(embeddings) {
		return "", false, fmt.Errorf("chunks and embeddings length mismatch")
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback(ctx) // no-op after successful Commit

	// Check if a document with this title already exists.
	var existingID string
	if err := tx.QueryRow(ctx,
		"SELECT id FROM documents WHERE title = $1", title,
	).Scan(&existingID); err != nil && err != pgx.ErrNoRows {
		return "", false, err
	}

	// Insert or update the document row.
	var docID string
	if existingID != "" {
		if err := tx.QueryRow(ctx,
			`UPDATE documents SET content = $2 WHERE title = $1 RETURNING id`,
			title, content,
		).Scan(&docID); err != nil {
			return "", false, err
		}
	} else {
		if err := tx.QueryRow(ctx,
			"INSERT INTO documents (title, content) VALUES ($1, $2) RETURNING id",
			title, content,
		).Scan(&docID); err != nil {
			return "", false, err
		}
	}

	// Delete existing chunks (cascades to embeddings).
	if _, err := tx.Exec(ctx,
		"DELETE FROM chunks WHERE document_id = $1", docID,
	); err != nil {
		return "", false, err
	}

	// Insert chunks in a batch.
	batch := &pgx.Batch{}
	for i, ch := range chunks {
		batch.Queue(
			"INSERT INTO chunks (document_id, content, position) VALUES ($1, $2, $3) RETURNING id",
			docID, ch, i,
		)
	}
	br := tx.SendBatch(ctx, batch)
	chunkIDs := make([]int64, 0, len(chunks))
	for range chunks {
		var id int64
		if err := br.QueryRow().Scan(&id); err != nil {
			br.Close()
			return "", false, err
		}
		chunkIDs = append(chunkIDs, id)
	}
	if err := br.Close(); err != nil {
		return "", false, err
	}

	// Insert embeddings.
	for i, id := range chunkIDs {
		if _, err := tx.Exec(ctx,
			"INSERT INTO embeddings (chunk_id, embedding) VALUES ($1, $2)",
			id, embeddings[i],
		); err != nil {
			return "", false, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return "", false, err
	}
	return docID, existingID == "", nil
}
