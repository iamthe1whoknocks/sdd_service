// Package middleware provides Echo middleware functions for logging, panic
// recovery, request ID injection and request timeouts.
package middleware

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"vulnrag/internal/httperr"
)

// Logger logs each request with method, path, status, duration, and request ID.
func Logger(logger *zap.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()

			err := next(c)

			duration := time.Since(start)
			requestID := c.Get(requestIDKey)

			// Extract the real HTTP status code.
			// Echo defers setting Response.Status until the response is actually
			// written, so c.Response().Status may still be 200 after next(c).
			// We extract the status from *echo.HTTPError first, then fall back
			// to Response.Status if the response was already committed.
			status := http.StatusOK
			if he, ok := err.(*echo.HTTPError); ok {
				status = he.Code
			} else if c.Response().Committed {
				status = c.Response().Status
			}

			var res string
			switch {
			case status >= 200 && status < 300:
				res = "OK"
			case status >= 400 && status < 500:
				res = "ERR"
			case status >= 500:
				res = "SERR"
			default:
				res = "???"
			}

			logger.Info("request",
				zap.String("res", res),
				zap.String("method", c.Request().Method),
				zap.String("path", c.Request().URL.Path),
				zap.Int("status", status),
				zap.Duration("duration", duration.Round(time.Millisecond)),
				zap.Any("requestID", requestID),
			)

			return err
		}
	}
}

// Recover catches panics and returns 500 Internal Server Error.
func Recover(logger *zap.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			defer func() {
				if r := recover(); r != nil {
					err, ok := r.(error)
					if !ok {
						err = echo.NewHTTPError(http.StatusInternalServerError, r)
					}
					logger.Error("PANIC recovered", zap.Any("error", err))
					_ = httperr.Write(c, http.StatusInternalServerError, "internal_error", "internal server error")
				}
			}()
			return next(c)
		}
	}
}
