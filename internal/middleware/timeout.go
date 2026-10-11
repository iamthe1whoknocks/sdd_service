package middleware

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// RequestSize limits the maximum request body size.
func RequestSize(maxBytes int64) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if c.Request().ContentLength > maxBytes {
				return &echo.HTTPError{
					Code:    http.StatusRequestEntityTooLarge,
					Message: "payload too large: content exceeds 1 MB limit",
				}
			}
			return next(c)
		}
	}
}
