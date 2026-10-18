package middleware

import (
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// RequestID generates a unique X-Request-ID for each request and injects it
// into the context for downstream logging.
func RequestID() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			id := uuid.New().String()
			c.Response().Header().Set(echo.HeaderXRequestID, id)
			c.Set("requestID", id)
			return next(c)
		}
	}
}


