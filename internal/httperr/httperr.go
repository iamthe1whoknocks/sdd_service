// Package httperr provides a unified API error format:
// {"error": {"type": "...", "detail": "..."}}.
//
// Contract defined in api/openapi.yaml (Error schema) and docs/spec.md §5.
// All error sources (handlers, middleware, recover) must
// return responses through this package so clients (SPA) can
// uniformly read error.detail.
package httperr

import (
	"github.com/labstack/echo/v4"
)

// Body — error body per OpenAPI Error schema.
type Body struct {
	Error Detail `json:"error"`
}

// Detail — error details (type/detail per RFC 7807-like convention).
type Detail struct {
	Type   string `json:"type,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Write returns an HTTP error response in a unified contract format.
func Write(c echo.Context, code int, etype, detail string) error {
	return c.JSON(code, Body{Error: Detail{Type: etype, Detail: detail}})
}
