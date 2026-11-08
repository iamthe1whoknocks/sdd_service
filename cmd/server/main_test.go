package main

import (
	"testing"

	"vulnrag/internal/rag"
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
