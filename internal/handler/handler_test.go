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
)

// fakeEngine — фейковая реализация Asker/Indexer для unit-тестов.
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

// api объединяет оба хендлера для регистрации маршрутов.
type api struct {
	ask  *handler.AskHandler
	docs *handler.DocumentsHandler
}

func (a *api) PostApiV1Ask(c echo.Context) error       { return a.ask.PostApiV1Ask(c) }
func (a *api) PostApiV1Documents(c echo.Context) error { return a.docs.PostApiV1Documents(c) }
func (a *api) GetHealth(c echo.Context) error          { return c.NoContent(http.StatusOK) }

// newRouter собирает echo-роутер с фейковым движком.
func newRouter(engine *fakeEngine) *echo.Echo {
	e := echo.New()
	handler.RegisterHandlers(e, &api{
		ask:  &handler.AskHandler{Engine: engine},
		docs: &handler.DocumentsHandler{Engine: engine},
	})
	return e
}

// doJSON выполняет запрос и возвращает статус и тело ответа.
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

// TestErrorShape проверяет контрактный формат ошибки из api/openapi.yaml:
// тело обязано быть {"error": {"type": ..., "detail": ...}}.
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
			return "", nil, errors.New("context deadline exceeded")
		},
	})
	status, body := doJSON(t, e, "/api/v1/ask", `{"question":"долгий вопрос"}`)
	if status != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504 (body=%v)", status, body)
	}
}

func TestAskEngineError(t *testing.T) {
	e := newRouter(&fakeEngine{
		askFn: func(_ context.Context, _ string) (string, []repository.SearchResult, error) {
			return "", nil, errors.New("ollama is down")
		},
	})
	status, _ := doJSON(t, e, "/api/v1/ask", `{"question":"вопрос"}`)
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", status)
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

func TestDocumentsIndexError(t *testing.T) {
	e := newRouter(&fakeEngine{
		idxFn: func(_ context.Context, _, _ string) (string, error) {
			return "", errors.New("embedding failed")
		},
	})
	status, _ := doJSON(t, e, "/api/v1/documents",
		`{"title":"ok","content":"some content"}`)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", status)
	}
}
