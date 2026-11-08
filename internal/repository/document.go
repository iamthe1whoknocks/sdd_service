package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

// ContentAndIDByTitle returns the id and stored content of the document with
// the given title.  The last result reports whether such a document exists.
// The RAG layer uses it as a cheap pre-check before generating embeddings
// (docs/TODO_SIGNIFICANT.md SIG-2): when the content is unchanged there is
// nothing to recompute, and the caller still needs the id to report the skip.
//
// The check is best effort: another writer may change the row before the
// subsequent upsert.  For the single-instance background indexing this race is
// harmless — the upsert still converges to the correct state.
func (r *DB) ContentAndIDByTitle(ctx context.Context, title string) (id, content string, exists bool, err error) {
	err = r.pool.QueryRow(ctx,
		"SELECT id, content FROM documents WHERE title = $1", title,
	).Scan(&id, &content)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("select document content by title: %w", err)
	}
	return id, content, true, nil
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

	if err := insertDocChunksAndEmbeddings(ctx, tx, docID, chunks, embeddings); err != nil {
		return "", err
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
//
// Uses a single INSERT … ON CONFLICT statement to avoid the TOCTOU race
// (docs/TODO_SIGNIFICANT.md SIG-3): two concurrent calls with the same title
// no longer collide with a unique-violation — the second one cleanly becomes
// an update.
func (r *DB) UpsertDocumentWithChunks(ctx context.Context, title, content string, chunks []string, embeddings [][]float32) (string, bool, error) {
	if len(chunks) != len(embeddings) {
		return "", false, fmt.Errorf("chunks and embeddings length mismatch")
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback(ctx) // no-op after successful Commit

	// Single atomic upsert: INSERT or UPDATE, plus a bool indicating whether
	// a row was inserted (xmax = 0 means no other transaction deleted it
	// before our XID committed — for a plain INSERT this is always true).
	var docID string
	var created bool
	if err := tx.QueryRow(ctx,
		`INSERT INTO documents (title, content) VALUES ($1, $2)
		 ON CONFLICT (title) DO UPDATE
			SET content = EXCLUDED.content
			RETURNING id, (xmax = 0) AS created`,
		title, content,
	).Scan(&docID, &created); err != nil {
		return "", false, err
	}

	// Delete existing chunks (cascades to embeddings).
	if _, err := tx.Exec(ctx,
		"DELETE FROM chunks WHERE document_id = $1", docID,
	); err != nil {
		return "", false, err
	}

	if err := insertDocChunksAndEmbeddings(ctx, tx, docID, chunks, embeddings); err != nil {
		return "", false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", false, err
	}
	return docID, created, nil
}

// insertDocChunksAndEmbeddings inserts chunks and embeddings for a document
// inside an already-started transaction.  Chunks are batched via pgx.Batch,
// embeddings use multi-row VALUES (docs/FIXES.md #16).
func insertDocChunksAndEmbeddings(ctx context.Context, tx pgx.Tx, docID string, chunks []string, embeddings [][]float32) error {
	// Batch-insert chunks.
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
			return err
		}
		chunkIDs = append(chunkIDs, id)
	}
	if err := br.Close(); err != nil {
		return err
	}

	// Batch-insert embeddings using multi-row VALUES.
	if err := insertEmbeddingsBatch(ctx, tx, chunkIDs, embeddings); err != nil {
		return err
	}
	return nil
}

// maxEmbeddingRowsPerStatement caps the number of rows in a single multi-row
// VALUES statement to keep the parameter count well below the 65535 limit of
// the PostgreSQL extended protocol.
const maxEmbeddingRowsPerStatement = 256

// buildEmbeddingInsert returns a multi-row INSERT statement and its arguments
// for chunkIDs[from:to].  Each row is a ($n, $n+1) pair, so a document with N
// chunks costs one statement instead of N round-trips.
//
// Each []float32 is sent as a single parameter; PostgreSQL infers the target
// type from the column and converts the float4 array literal via the pgvector
// input function, exactly as the previous per-row INSERT did.
func buildEmbeddingInsert(chunkIDs []int64, embeddings [][]float32, from, to int) (string, []any) {
	var sb strings.Builder
	sb.WriteString("INSERT INTO embeddings (chunk_id, embedding) VALUES ")
	args := make([]any, 0, (to-from)*2)
	for i := from; i < to; i++ {
		if i > from {
			sb.WriteString(",")
		}
		// Parameter numbers are local to the statement and start at $1.
		sb.WriteString(fmt.Sprintf("($%d,$%d)", (i-from)*2+1, (i-from)*2+2))
		args = append(args, chunkIDs[i], embeddings[i])
	}
	return sb.String(), args
}

// insertEmbeddingsBatch inserts one row per (chunkID, embedding) pair using
// multi-row VALUES statements instead of a per-row Exec (docs/FIXES.md #16).
// It runs inside the caller's transaction, so document, chunks and embeddings
// stay atomic.  Rows are grouped to stay clear of the protocol parameter limit.
func insertEmbeddingsBatch(ctx context.Context, tx pgx.Tx, chunkIDs []int64, embeddings [][]float32) error {
	for start := 0; start < len(chunkIDs); start += maxEmbeddingRowsPerStatement {
		end := min(start+maxEmbeddingRowsPerStatement, len(chunkIDs))
		query, args := buildEmbeddingInsert(chunkIDs, embeddings, start, end)
		// A vector of the wrong dimension is rejected by the vector(768)
		// column, so no extra client-side check is needed here.
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			return err
		}
	}
	return nil
}
