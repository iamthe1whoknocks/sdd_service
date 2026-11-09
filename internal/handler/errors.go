package handler

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"vulnrag/internal/httperr"
	"vulnrag/internal/validate"
)

// respondError writes an error in the unified OpenAPI contract format:
// {"error": {"type": "...", "detail": "..."}}. The format is defined in
// api/openapi.yaml (Error schema); see also docs/spec.md §5.
func respondError(c echo.Context, code int, etype, detail string) error {
	return httperr.Write(c, code, etype, detail)
}

// respondValidationError maps a validation error to the contract response:
// 413 for byte-limit violations, 422 for field-level violations
// (docs/spec.md §4).
func respondValidationError(c echo.Context, err error) error {
	var ve *validate.Error
	if errors.As(err, &ve) && ve.TooLarge {
		return respondError(c, http.StatusRequestEntityTooLarge, "payload_too_large", ve.Message)
	}
	return respondError(c, http.StatusUnprocessableEntity, "validation_error", err.Error())
}

// requestID returns the request-scoped identifier set by the RequestID
// middleware (internal/middleware/requestid.go).
func requestID(c echo.Context) string {
	if v, ok := c.Get("requestID").(string); ok {
		return v
	}
	return ""
}
