package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vulnrag/internal/rag"

	"go.uber.org/zap"
)

// TestUpsertOutcome is the regression guard for the counting rules of
// docs/TODO_SIGNIFICANT.md SIG-2.  cmd/server had no tests, so a row whose
// embeddings were recomputed could be logged as a skipped computation (and the
// other way round) without any check noticing.
func TestUpsertOutcome(t *testing.T) {
	tests := []struct {
		name string
		res  rag.UpsertResult
		want string
	}{
		{
			name: "new document is counted as indexed work",
			res:  rag.UpsertResult{DocID: "1", Created: true},
			want: "indexed",
		},
		{
			// The case SIG-2 is about: content matched, Ollama was never called.
			name: "identical content is counted as skipped",
			res:  rag.UpsertResult{DocID: "1", Unchanged: true},
			want: "unchanged",
		},
		{
			// Created=false without Unchanged means the repository updated an
			// existing row, so embeddings were regenerated: not a skip.
			name: "changed content is counted as re-indexed",
			res:  rag.UpsertResult{DocID: "1"},
			want: "updated",
		},
		{
			// Unchanged wins: reporting skipped work as done is the failure
			// this guard exists for, so it must not be reachable via Created.
			name: "unchanged takes precedence over created",
			res:  rag.UpsertResult{DocID: "1", Created: true, Unchanged: true},
			want: "unchanged",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := upsertOutcome(tt.res); got != tt.want {
				t.Errorf("upsertOutcome(%+v) = %q, want %q", tt.res, got, tt.want)
			}
		})
	}
}

// TestVerifyEmbeddingDimensionRejectsWrongModel is the regression guard for
// docs/TODO_SIGNIFICANT.md SIG-5 criterion 3: the startup probe must still
// reject a model whose vector size does not match the vector(N) column.
//
// The client is built by NewOllamaClient with OLLAMA_HOST pointed at a stub, so
// the check exercises the real HTTP path rather than a Go-level fake.  The stub
// answers the single-text /api/embeddings endpoint that the probe uses.
func TestVerifyEmbeddingDimensionRejectsWrongModel(t *testing.T) {
	t.Setenv("OLLAMA_HOST", embeddingStub(t, `{"embedding":[0.1,0.2]}`))

	err := verifyEmbeddingDimension(context.Background(), rag.NewOllamaClient(), 768, zap.NewNop())
	if err == nil {
		t.Fatal("verifyEmbeddingDimension accepted a 2-dim model for a 768-dim column")
	}
	// The message must name both dimensions: it is the operator's only hint
	// about whether to fix the migration or the model.
	for _, want := range []string{"2", "768"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// TestVerifyEmbeddingDimensionAcceptsMatchingModel pins the other half: a model
// of the right size must pass, otherwise startup would fail on every correct
// configuration.
func TestVerifyEmbeddingDimensionAcceptsMatchingModel(t *testing.T) {
	t.Setenv("OLLAMA_HOST", embeddingStub(t, `{"embedding":[0.1,0.2,0.3]}`))

	if err := verifyEmbeddingDimension(context.Background(), rag.NewOllamaClient(), 3, zap.NewNop()); err != nil {
		t.Fatalf("verifyEmbeddingDimension = %v, want nil for a matching model", err)
	}
}

// embeddingStub starts an Ollama stub answering /api/embeddings with body and
// returns its URL.
func embeddingStub(t *testing.T, body string) string {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)

	return srv.URL
}

// TestUpsertOutcomeCoversEveryResult makes sure the three names the counters in
// initVulnlist are keyed by are exactly the ones upsertOutcome can return.
// A typo in either place would silently drop rows from the summary.
func TestUpsertOutcomeCoversEveryResult(t *testing.T) {
	seen := map[string]bool{}
	for _, res := range []rag.UpsertResult{
		{Created: true},
		{Unchanged: true},
		{Created: true, Unchanged: true},
		{},
	} {
		seen[upsertOutcome(res)] = true
	}
	for _, want := range []string{"indexed", "updated", "unchanged"} {
		if !seen[want] {
			t.Errorf("upsertOutcome never returns %q, but initVulnlist counts it", want)
		}
	}
	if len(seen) != 3 {
		t.Errorf("upsertOutcome returned %d distinct outcomes, want 3: %v", len(seen), seen)
	}
}
