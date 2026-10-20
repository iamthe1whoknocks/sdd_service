package handler

import (
	"github.com/labstack/echo/v4"

	"vulnrag/internal/httperr"
)

// respondError writes an error in the unified OpenAPI contract format:
// {"error": {"type": "...", "detail": "..."}}. The format is defined in
// api/openapi.yaml (Error schema); see also docs/spec.md §5.
func respondError(c echo.Context, code int, etype, detail string) error {
	return httperr.Write(c, code, etype, detail)
}

// requestID returns the request-scoped identifier set by the RequestID
// middleware (internal/middleware/requestid.go).
func requestID(c echo.Context) string {
	if v, ok := c.Get("requestID").(string); ok {
		return v
	}
	return ""
}
