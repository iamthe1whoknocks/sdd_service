package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"vulnrag/internal/handler"
	"vulnrag/internal/repository"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// fakeEngine is a fake Asker/Indexer implementation for unit tests.
type fakeEngine struct {
	askFn func(ctx context.Context, q string) (string, []repository.SearchResult, error)
	idxFn func(ctx context.Context, title, content string) (string, error)
}

func (f *fakeEngine) Ask(ctx context.Context, q string) (string, []repository.SearchResult, error) {
	return f.askFn(ctx, q)
}

func (f *fakeEngine) IndexDocument(ctx context.Context, title, content string) (string, error) {
	return f.idxFn(ctx, title, content)
}

// api bundles both handlers for route registration.
type api struct {
	ask  *handler.AskHandler
	docs *handler.DocumentsHandler
}

func (a *api) PostApiV1Ask(c echo.Context) error       { return a.ask.PostApiV1Ask(c) }
func (a *api) PostApiV1Documents(c echo.Context) error { return a.docs.PostApiV1Documents(c) }
func (a *api) GetHealth(c echo.Context) error          { return c.NoContent(http.StatusOK) }

// newRouter assembles an echo router backed by a fake engine.
func newRouter(engine *fakeEngine) *echo.Echo {
	e := echo.New()
	log := zap.NewNop()
	handler.RegisterHandlers(e, &api{
		ask:  handler.NewAskHandler(engine, log),
		docs: handler.NewDocumentsHandler(engine, log),
	})
	return e
}

// doJSON performs a request and returns the status code and the response body.
func doJSON(t *testing.T, e *echo.Echo, path, body string) (int, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	var parsed map[string]interface{}
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
			t.Fatalf("invalid JSON response: %v (body=%s)", err, rec.Body.String())
		}
	}
	return rec.Code, parsed
}

func okEngine() *fakeEngine {
	return &fakeEngine{
		askFn: func(_ context.Context, q string) (string, []repository.SearchResult, error) {
			return "Log4Shell — уязвимость в Apache Log4j2 (CVE-2021-44228)",
				[]repository.SearchResult{{Title: "CVE-2021-44228", Content: "JNDI lookup"}}, nil
		},
		idxFn: func(_ context.Context, title, content string) (string, error) {
			return "6ba7b810-9dad-11d1-80b4-00c04fd430c8", nil
		},
	}
}

// errorDetail extracts the error.detail string from a response body.
func errorDetail(body map[string]interface{}) string {
	errObj, ok := body["error"].(map[string]interface{})
	if !ok {
		return ""
	}
	detail, _ := errObj["detail"].(string)
	return detail
}

// --- POST /api/v1/ask ---

func TestAskSuccess(t *testing.T) {
	_, body := doJSON(t, newRouter(okEngine()), "/api/v1/ask",
		`{"question":"что такое Log4Shell?"}`)

	if body["answer"] != "Log4Shell — уязвимость в Apache Log4j2 (CVE-2021-44228)" {
		t.Errorf("answer = %v, want текст про Log4Shell", body["answer"])
	}
	sources, ok := body["sources"].([]interface{})
	if !ok || len(sources) == 0 {
		t.Fatalf("sources не найден: %#v", body["sources"])
	}
}

func TestAskValidation(t *testing.T) {
	e := newRouter(okEngine())
	cases := []struct {
		name string
		body string
		want int
	}{
		{"пустой вопрос", `{"question":""}`, http.StatusUnprocessableEntity},
		{"вопрос из пробелов", `{"question":"   "}`, http.StatusUnprocessableEntity},
		{"вопрос длиннее 4000 символов", `{"question":"` + strings.Repeat("а", 4001) + `"}`, http.StatusUnprocessableEntity},
		{"битый JSON", `{broken`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := doJSON(t, e, "/api/v1/ask", tc.body)
			if status != tc.want {
				t.Fatalf("status = %d, want %d (body=%v)", status, tc.want, body)
			}
			if errBody, ok := body["error"].(map[string]interface{}); !ok || errBody["detail"] == nil {
				t.Errorf("в теле ошибки нет error.detail: %v", body)
			}
		})
	}
}

// TestErrorShape checks the error format contract from api/openapi.yaml:
// the body must be {"error": {"type": ..., "detail": ...}}.
func TestErrorShape(t *testing.T) {
	_, body := doJSON(t, newRouter(okEngine()), "/api/v1/ask", `{"question":""}`)

	errObj, ok := body["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("нет обёртки error: %v", body)
	}
	if _, ok := errObj["type"]; !ok {
		t.Errorf("нет error.type: %v", body)
	}
	if _, ok := errObj["detail"]; !ok {
		t.Errorf("нет error.detail: %v", body)
	}
}

func TestAskTimeout(t *testing.T) {
	e := newRouter(&fakeEngine{
		askFn: func(_ context.Context, _ string) (string, []repository.SearchResult, error) {
			return "", nil, context.DeadlineExceeded
		},
	})
	status, body := doJSON(t, e, "/api/v1/ask", `{"question":"долгий вопрос"}`)
	if status != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504 (body=%v)", status, body)
	}
	if detail := errorDetail(body); detail != "generation timed out after 60 seconds" {
		t.Errorf("detail = %q, want %q", detail, "generation timed out after 60 seconds")
	}
}

// TestAskEngineError checks that an internal engine error returns 500 and the
// response does not leak the internal error details (docs/FIXES.md #2).
func TestAskEngineError(t *testing.T) {
	const internalErr = "ollama is down: connection refused to 10.0.0.4:11434"
	e := newRouter(&fakeEngine{
		askFn: func(_ context.Context, _ string) (string, []repository.SearchResult, error) {
			return "", nil, errors.New(internalErr)
		},
	})
	status, body := doJSON(t, e, "/api/v1/ask", `{"question":"вопрос"}`)
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", status)
	}
	detail := errorDetail(body)
	if detail != "internal server error" {
		t.Errorf("detail = %q, want generic message", detail)
	}
	if strings.Contains(detail, internalErr) {
		t.Errorf("detail утекает внутреннюю ошибку: %q", detail)
	}
}

// TestAskNoRelevantData checks that a question with no chunks above the
// relevance floor returns 200 with a fixed answer and an empty sources list
// instead of 500 (docs/FIXES.md #9).
func TestAskNoRelevantData(t *testing.T) {
	e := newRouter(&fakeEngine{
		askFn: func(_ context.Context, _ string) (string, []repository.SearchResult, error) {
			return "Релевантных данных в базе знаний не найдено.", nil, nil
		},
	})
	status, body := doJSON(t, e, "/api/v1/ask", `{"question":"рецепт блинов"}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%v)", status, body)
	}
	if body["answer"] != "Релевантных данных в базе знаний не найдено." {
		t.Errorf("answer = %v, want 'нет релевантных данных'", body["answer"])
	}
	sources, ok := body["sources"].([]interface{})
	if !ok || len(sources) != 0 {
		t.Errorf("sources = %v, want empty list", body["sources"])
	}
}

// --- POST /api/v1/documents ---

func TestDocumentsSuccess(t *testing.T) {
	e := newRouter(okEngine())
	status, body := doJSON(t, e, "/api/v1/documents",
		`{"title":"CVE-2021-44228","content":"Apache Log4j2 JNDI lookup"}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%v)", status, body)
	}
	docID, _ := body["document_id"].(string)
	uuidRe := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	if !uuidRe.MatchString(docID) {
		t.Errorf("document_id = %q, не UUID", docID)
	}
	if indexed, _ := body["indexed"].(bool); !indexed {
		t.Errorf("indexed = %v, want true", body["indexed"])
	}
}

func TestDocumentsValidation(t *testing.T) {
	e := newRouter(okEngine())
	cases := []struct {
		name string
		body string
		want int
	}{
		{"пустой title", `{"title":"","content":"text"}`, http.StatusUnprocessableEntity},
		{"title из пробелов", `{"title":"   ","content":"text"}`, http.StatusUnprocessableEntity},
		{"title длиннее 500 символов", `{"title":"` + strings.Repeat("т", 501) + `","content":"text"}`, http.StatusUnprocessableEntity},
		{"контент больше 1 МБ", `{"title":"ok","content":"` + strings.Repeat("a", 2*1024*1024) + `"}`, http.StatusRequestEntityTooLarge},
		{"пустой content", `{"title":"ok","content":""}`, http.StatusUnprocessableEntity},
		{"битый JSON", `{broken`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, _ := doJSON(t, e, "/api/v1/documents", tc.body)
			if status != tc.want {
				t.Fatalf("status = %d, want %d", status, tc.want)
			}
		})
	}
}

// TestDocumentsIndexError checks that an indexing failure returns 500 (an
// internal server error, not a client validation problem) and does not leak
// the internal error details to the client (docs/FIXES.md #2, #3).
func TestDocumentsIndexError(t *testing.T) {
	const internalErr = "embedding failed: ollama returned HTTP 503"
	e := newRouter(&fakeEngine{
		idxFn: func(_ context.Context, _, _ string) (string, error) {
			return "", errors.New(internalErr)
		},
	})
	status, body := doJSON(t, e, "/api/v1/documents",
		`{"title":"ok","content":"some content"}`)
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", status)
	}
	detail := errorDetail(body)
	if detail != "document indexing failed" {
		t.Errorf("detail = %q, want generic message", detail)
	}
	if strings.Contains(detail, internalErr) {
		t.Errorf("detail утекает внутреннюю ошибку: %q", detail)
	}
}

// TestDocumentsInvalidID checks that a non-UUID document ID from the engine
// returns 500 without leaking internal details.
func TestDocumentsInvalidID(t *testing.T) {
	e := newRouter(&fakeEngine{
		idxFn: func(_ context.Context, _, _ string) (string, error) {
			return "not-a-uuid", nil
		},
	})
	status, body := doJSON(t, e, "/api/v1/documents",
		`{"title":"ok","content":"some content"}`)
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", status)
	}
	if detail := errorDetail(body); detail != "invalid document ID" {
		t.Errorf("detail = %q, want %q", detail, "invalid document ID")
	}
}
