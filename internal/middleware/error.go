// Package middleware. Собственный формат ошибок больше не определяется здесь:
// единый контрактный формат {error: {type, detail}} вынесен в internal/httperr
// (см. api/openapi.yaml, схема Error).
package middleware
