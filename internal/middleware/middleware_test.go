package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
