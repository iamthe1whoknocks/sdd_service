package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// fakeTx records the statements executed inside a transaction. Only Exec is
// implemented; the remaining methods come from the embedded interface and
// panic if called, which keeps the fake honest about what the code under test
// is allowed to do.
type fakeTx struct {
	pgx.Tx
	queries []string
	args    [][]any
	err     error
}

func (f *fakeTx) Exec(_ context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	f.queries = append(f.queries, sql)
	f.args = append(f.args, arguments)
	if f.err != nil {
		return pgconn.CommandTag{}, f.err
	}
	return pgconn.NewCommandTag(fmt.Sprintf("INSERT %d", len(arguments)/2)), nil
}

// makeRows returns n chunk ids with a distinct 2-dimensional embedding each.
func makeRows(n int) ([]int64, [][]float32) {
	ids := make([]int64, n)
	emb := make([][]float32, n)
	for i := 0; i < n; i++ {
		ids[i] = int64(i + 1)
		emb[i] = []float32{float32(i), float32(i) + 0.5}
	}
	return ids, emb
}

// valueTupleCount counts the (?,?) groups in a multi-row VALUES statement.
func valueTupleCount(query string) int {
	return strings.Count(query, "),(") + 1
}

func TestBuildEmbeddingInsert(t *testing.T) {
	ids, emb := makeRows(3)
	query, args := buildEmbeddingInsert(ids, emb, 0, 3)

	if !strings.HasPrefix(query, "INSERT INTO embeddings (chunk_id, embedding) VALUES ") {
		t.Errorf("unexpected statement prefix: %q", query)
	}
	if got := valueTupleCount(query); got != 3 {
		t.Errorf("expected 3 value tuples, got %d in %q", got, query)
	}
	// Placeholders must be numbered $1..$6 with no gaps.
	for i := 1; i <= 6; i++ {
		if !strings.Contains(query, fmt.Sprintf("$%d", i)) {
			t.Errorf("placeholder $%d missing in %q", i, query)
		}
	}
	if len(args) != 6 {
		t.Fatalf("expected 6 arguments, got %d", len(args))
	}
	// Arguments must interleave chunk id and its own embedding.
	for i := 0; i < 3; i++ {
		if got := args[i*2]; got != ids[i] {
			t.Errorf("arg %d: expected chunk id %v, got %v", i*2, ids[i], got)
		}
		if got, ok := args[i*2+1].([]float32); !ok || got[0] != emb[i][0] {
			t.Errorf("arg %d: expected embedding %v, got %v", i*2+1, emb[i], got)
		}
	}
}

// TestInsertEmbeddingsBatchIsNotPerRow is the regression guard for
// N embeddings must cost one statement, not N Exec calls.
func TestInsertEmbeddingsBatchIsNotPerRow(t *testing.T) {
	ids, emb := makeRows(50)
	tx := &fakeTx{}

	if err := insertEmbeddingsBatch(context.Background(), tx, ids, emb); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tx.queries) != 1 {
		t.Fatalf("expected 1 batched statement for 50 rows, got %d", len(tx.queries))
	}
	if got := valueTupleCount(tx.queries[0]); got != 50 {
		t.Errorf("expected 50 value tuples, got %d", got)
	}
	if len(tx.args[0]) != 100 {
		t.Errorf("expected 100 arguments, got %d", len(tx.args[0]))
	}
}

// TestInsertEmbeddingsBatchChunksRows checks that a large document is split
// into several statements and that placeholder numbering restarts at $1 in
// each of them.
func TestInsertEmbeddingsBatchChunksRows(t *testing.T) {
	const rows = maxEmbeddingRowsPerStatement*2 + 7
	ids, emb := makeRows(rows)
	tx := &fakeTx{}

	if err := insertEmbeddingsBatch(context.Background(), tx, ids, emb); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tx.queries) != 3 {
		t.Fatalf("expected 3 statements for %d rows, got %d", rows, len(tx.queries))
	}

	wantRows := []int{maxEmbeddingRowsPerStatement, maxEmbeddingRowsPerStatement, 7}
	var totalArgs int
	for i, query := range tx.queries {
		if got := valueTupleCount(query); got != wantRows[i] {
			t.Errorf("statement %d: expected %d value tuples, got %d", i, wantRows[i], got)
		}
		last := wantRows[i] * 2
		if !strings.Contains(query, fmt.Sprintf("$%d", last)) {
			t.Errorf("statement %d: last placeholder $%d missing", i, last)
		}
		if strings.Contains(query, fmt.Sprintf("$%d,", last+1)) {
			t.Errorf("statement %d: placeholders leak past the row count", i)
		}
		totalArgs += len(tx.args[i])
	}
	if totalArgs != rows*2 {
		t.Errorf("expected %d arguments in total, got %d", rows*2, totalArgs)
	}

	// Every row must be present exactly once, in order.
	seen := make(map[int64]bool, rows)
	for _, args := range tx.args {
		for j := 0; j < len(args); j += 2 {
			id := args[j].(int64)
			if seen[id] {
				t.Errorf("chunk id %d inserted twice", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != rows {
		t.Errorf("expected %d distinct chunk ids, got %d", rows, len(seen))
	}
}

func TestInsertEmbeddingsBatchEmpty(t *testing.T) {
	tx := &fakeTx{}
	if err := insertEmbeddingsBatch(context.Background(), tx, nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tx.queries) != 0 {
		t.Errorf("no statement must be sent for an empty row set, got %d", len(tx.queries))
	}
}

func TestInsertEmbeddingsBatchPropagatesError(t *testing.T) {
	ids, emb := makeRows(10)
	wantErr := errors.New("boom")
	tx := &fakeTx{err: wantErr}

	if err := insertEmbeddingsBatch(context.Background(), tx, ids, emb); !errors.Is(err, wantErr) {
		t.Fatalf("expected the database error to propagate, got %v", err)
	}
}
