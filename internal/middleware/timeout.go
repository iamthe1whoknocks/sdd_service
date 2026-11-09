package middleware

import (
	"context"
	"time"

	"github.com/labstack/echo/v4"
)

// Timeout sets a deadline on the request context so that the whole handler
// pipeline (RAG retrieve -> generate) is bounded (docs/spec.md §4, §9).
//
// The middleware itself does not write a response: the handler observes the
// deadline via errors.Is(err, context.DeadlineExceeded) and maps it to a 504
// Gateway Timeout. This keeps the status code and the
// error contract under the handler's control instead of relying on a
// fixed-status middleware.
func Timeout(timeout time.Duration) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx, cancel := context.WithTimeout(c.Request().Context(), timeout)
			defer cancel()
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
}