package rag

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// hangingOllama starts a stub that never answers within the test's lifetime:
// the handler blocks until the per-call cap cancels the request.  This
// emulates a stuck Ollama process, the exact failure
// docs/TODO_SIGNIFICANT.md SIG-4 is about.
//
// The release channel unblocks the handler during cleanup so that srv.Close(),
// which waits for outstanding requests, returns immediately instead of
// waiting out an arbitrary sleep.
func hangingOllama(t *testing.T) *httptest.Server {
	t.Helper()

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})
	return srv
}

// TestGenerateEmbeddingIsBoundedByCallCap is the regression guard for SIG-4:
// a hanging Ollama must not hang the caller forever.  The client carries no
// HTTP timeout and the caller context has no deadline, so the per-call cap is
// the only thing that can end this call.
func TestGenerateEmbeddingIsBoundedByCallCap(t *testing.T) {
	srv := hangingOllama(t)
	c := &OllamaClient{
		BaseURL:     srv.URL,
		EmbedModel:  "fake-embed",
		client:      srv.Client(),
		callTimeout: 50 * time.Millisecond,
	}

	start := time.Now()
	_, err := c.GenerateEmbedding(context.Background(), "text")
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	// Generous upper bound: the assertion is about the cap firing at all, not
	// about scheduler precision.
	if elapsed > 2*time.Second {
		t.Errorf("call took %v, want it bounded by the 50ms cap", elapsed)
	}
}

// TestGenerateAnswerIsBoundedByCallCap covers the LLM path, which is the slower
// of the two and the one most likely to stall.
func TestGenerateAnswerIsBoundedByCallCap(t *testing.T) {
	srv := hangingOllama(t)
	c := &OllamaClient{
		BaseURL:     srv.URL,
		LLMModel:    "fake-llm",
		client:      srv.Client(),
		callTimeout: 50 * time.Millisecond,
	}

	start := time.Now()
	_, err := c.GenerateAnswer(context.Background(), "prompt")
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("call took %v, want it bounded by the 50ms cap", elapsed)
	}
}

func TestWithCallTimeoutAppliesCapToBackgroundContext(t *testing.T) {
	c := &OllamaClient{callTimeout: 5 * time.Second}

	ctx, cancel := c.withCallTimeout(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("context has no deadline, want one derived from the cap")
	}
	if remaining := time.Until(deadline); remaining > 5*time.Second || remaining < 4*time.Second {
		t.Errorf("deadline in %v, want ~5s", remaining)
	}
}

// TestWithCallTimeoutNeverExtendsTighterCallerDeadline pins the min() semantics:
// the 60s /ask middleware must stay authoritative even when the per-call cap is
// larger, otherwise the cap would silently loosen the documented contract.
func TestWithCallTimeoutNeverExtendsTighterCallerDeadline(t *testing.T) {
	c := &OllamaClient{callTimeout: time.Hour}

	parent, parentCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer parentCancel()

	ctx, cancel := c.withCallTimeout(parent)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("context has no deadline")
	}
	parentDeadline, _ := parent.Deadline()
	if !deadline.Equal(parentDeadline) {
		t.Errorf("deadline = %v, want the caller's %v (cap must not extend it)", deadline, parentDeadline)
	}
}

// TestWithCallTimeoutZeroValueFallsBackToDefault guards the hand-built
// OllamaClient used across tests: an unset cap must mean the documented
// default, not an already-expired deadline that fails every call instantly.
func TestWithCallTimeoutZeroValueFallsBackToDefault(t *testing.T) {
	c := &OllamaClient{}

	ctx, cancel := c.withCallTimeout(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("context has no deadline, want the default cap")
	}
	if remaining := time.Until(deadline); remaining > defaultCallTimeout || remaining < defaultCallTimeout-time.Minute {
		t.Errorf("deadline in %v, want ~%v (defaultCallTimeout)", remaining, defaultCallTimeout)
	}
}

func TestNewOllamaClientReadsCallTimeoutFromEnv(t *testing.T) {
	t.Setenv("OLLAMA_CALL_TIMEOUT", "45s")

	if got := NewOllamaClient().callTimeout; got != 45*time.Second {
		t.Errorf("callTimeout = %v, want 45s", got)
	}
}

func TestNewOllamaClientDefaultsCallTimeout(t *testing.T) {
	os.Unsetenv("OLLAMA_CALL_TIMEOUT")

	if got := NewOllamaClient().callTimeout; got != defaultCallTimeout {
		t.Errorf("callTimeout = %v, want %v", got, defaultCallTimeout)
	}
}
