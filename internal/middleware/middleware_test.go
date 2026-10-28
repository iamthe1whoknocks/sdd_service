package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// assertErrorShape проверяет, что тело ответа содержит обёртку error
// контрактного формата {"error": {"type", "detail"}} (api/openapi.yaml).
func assertErrorShape(t *testing.T, status int, body string, wantStatus int) {
	t.Helper()
	if status != wantStatus {
		t.Fatalf("status = %d, want %d (body=%s)", status, wantStatus, body)
	}
	if !strings.Contains(body, `"error"`) {
		t.Errorf("в теле нет обёртки error: %s", body)
	}
	if !strings.Contains(body, `"detail"`) {
		t.Errorf("в теле нет error.detail: %s", body)
	}
}





func TestRecoverErrorShape(t *testing.T) {
	e := echo.New()
	e.Use(Recover(zap.NewNop()))
	e.GET("/boom", func(c echo.Context) error {
		panic("boom")
	})

	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assertErrorShape(t, rec.Code, rec.Body.String(), http.StatusInternalServerError)
}

// TestTimeoutPropagatesDeadline verifies that the Timeout middleware puts a
// deadline on the request context so that downstream code can observe it via
// ctx.Deadline() and errors.Is(ctx.Err(), context.DeadlineExceeded) (docs/FIXES.md #4).
func TestTimeoutPropagatesDeadline(t *testing.T) {
	e := echo.New()

	var hasDeadline bool
	e.GET("/deadline", func(c echo.Context) error {
		_, hasDeadline = c.Request().Context().Deadline()
		return nil
	}, Timeout(50*time.Millisecond))

	req := httptest.NewRequest(http.MethodGet, "/deadline", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !hasDeadline {
		t.Error("context has no deadline set by Timeout middleware")
	}
}

// --- RequestID (docs/FIXES.md #17) ---

// serveRequestID runs one request through the RequestID middleware and returns
// the ID echoed in the response header together with the ID the handler
// observed in the echo context. A nil inbound means the client sent no
// X-Request-ID at all.
func serveRequestID(t *testing.T, inbound *string) (header, fromContext string) {
	t.Helper()

	e := echo.New()
	var ctxID string
	e.Use(RequestID())
	e.GET("/ping", func(c echo.Context) error {
		ctxID, _ = c.Get(requestIDKey).(string)
		return c.String(http.StatusOK, "pong")
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	if inbound != nil {
		req.Header.Set(echo.HeaderXRequestID, *inbound)
	}

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	return rec.Header().Get(echo.HeaderXRequestID), ctxID
}

func TestRequestIDPropagatesInboundHeader(t *testing.T) {
	cases := []struct {
		name    string
		inbound string
	}{
		{"uuid", "3f2504e0-4f89-11d3-9a0c-0305e82c3301"},
		{"w3c trace id", "0af7651916cd43dd8448eb211c80319c"},
		{"prefixed", "svc-a:9b2d1c"},
		{"k8s style", "job-2026-10-28.run_1.final"},
		{"max length", strings.Repeat("a", maxRequestIDLen)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			header, ctxID := serveRequestID(t, &tc.inbound)
			if header != tc.inbound {
				t.Errorf("response header = %q, want the inbound value %q", header, tc.inbound)
			}
			if ctxID != tc.inbound {
				t.Errorf("context requestID = %q, want the inbound value %q", ctxID, tc.inbound)
			}
		})
	}
}

func TestRequestIDSurroundingWhitespaceIsTrimmed(t *testing.T) {
	inbound := "  req-42\t"
	header, ctxID := serveRequestID(t, &inbound)

	if header != "req-42" {
		t.Errorf("response header = %q, want %q", header, "req-42")
	}
	if ctxID != "req-42" {
		t.Errorf("context requestID = %q, want %q", ctxID, "req-42")
	}
}

// TestRequestIDGeneratesWhenAbsentOrUntrusted covers every input that must not
// be reflected back: the header is missing, blank, over the length limit, or
// carries characters that would corrupt the log or the response header.
func TestRequestIDGeneratesWhenAbsentOrUntrusted(t *testing.T) {
	cases := []struct {
		name    string
		inbound *string
	}{
		{"absent", nil},
		{"empty", strPtr("")},
		{"whitespace only", strPtr("   ")},
		{"too long", strPtr(strings.Repeat("a", maxRequestIDLen+1))},
		{"crlf log forging", strPtr("ok\r\nINFO fake entry")},
		{"inner space", strPtr("req 42")},
		{"markup", strPtr("<script>alert(1)</script>")},
		{"quote", strPtr(`a"b`)},
		{"slash", strPtr("a/b")},
		{"non ascii", strPtr("запрос-42")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			header, ctxID := serveRequestID(t, tc.inbound)

			if tc.inbound != nil && *tc.inbound != "" && strings.TrimSpace(*tc.inbound) == header {
				t.Fatalf("untrusted inbound value was reflected back: %q", header)
			}
			if _, err := uuid.Parse(header); err != nil {
				t.Errorf("response header = %q, want a generated UUID: %v", header, err)
			}
			if ctxID != header {
				t.Errorf("context requestID = %q, want it to match the header %q", ctxID, header)
			}
		})
	}
}

func TestRequestIDIsUniquePerRequest(t *testing.T) {
	first, _ := serveRequestID(t, nil)
	second, _ := serveRequestID(t, nil)

	if first == second {
		t.Errorf("two requests without an inbound ID share the same requestID %q", first)
	}
}

// TestRequestIDHeaderPresentOnErrorResponse guards the ordering inside the
// middleware: the header must be set before next(c), so a handler that commits
// an error response still carries the correlation ID.
func TestRequestIDHeaderPresentOnErrorResponse(t *testing.T) {
	const inbound = "req-failing-1"

	e := echo.New()
	e.Use(RequestID())
	e.POST("/boom", func(c echo.Context) error {
		return c.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "bad question"})
	})

	req := httptest.NewRequest(http.MethodPost, "/boom", strings.NewReader("{}"))
	req.Header.Set(echo.HeaderXRequestID, inbound)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if got := rec.Header().Get(echo.HeaderXRequestID); got != inbound {
		t.Errorf("response header on error = %q, want %q", got, inbound)
	}
}

func strPtr(s string) *string { return &s }
