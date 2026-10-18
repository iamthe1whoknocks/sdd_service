package handler

import (
	"github.com/labstack/echo/v4"

	"vulnrag/internal/httperr"
)

// respondError отдаёт ошибку в едином формате контракта OpenAPI:
// {"error": {"type": "...", "detail": "..."}}. Формат задан в
// api/openapi.yaml (схема Error); см. также docs/spec.md §5.
func respondError(c echo.Context, code int, etype, detail string) error {
	return httperr.Write(c, code, etype, detail)
}
