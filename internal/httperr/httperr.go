// Package httperr предоставляет единый формат ошибок API:
// {"error": {"type": "...", "detail": "..."}}.
//
// Контракт задан в api/openapi.yaml (схема Error) и docs/spec.md §5.
// Все источники ошибок (хендлеры, middleware, recover) обязаны
// отдавать ответы через этот пакет, чтобы клиенты (SPA) могли
// единообразно читать error.detail.
package httperr

import (
	"github.com/labstack/echo/v4"
)

// Body — тело ошибки по схеме Error из OpenAPI.
type Body struct {
	Error Detail `json:"error"`
}

// Detail — детали ошибки (type/detail по RFC 7807-подобному соглашению).
type Detail struct {
	Type   string `json:"type,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Write отдаёт HTTP-ответ с ошибкой в едином контрактном формате.
func Write(c echo.Context, code int, etype, detail string) error {
	return c.JSON(code, Body{Error: Detail{Type: etype, Detail: detail}})
}
