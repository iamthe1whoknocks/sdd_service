package middleware

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

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

// ErrGatewayTimeout returns a 504 Gateway Timeout error.
func ErrGatewayTimeout() error {
	return &ErrorResponse{
		Type:   strconv.Itoa(http.StatusInternalServerError),
		Detail: "Gateway Timeout",
	}
}

// TimeoutHandler wraps a handler with a context timeout.
func TimeoutHandler(timeout time.Duration) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx, cancel := context.WithTimeout(c.Request().Context(), timeout)
			defer cancel()

			req := c.Request().WithContext(ctx)
			c.SetRequest(req)

			done := make(chan error, 1)
			go func() {
				done <- next(c)
			}()

			select {
			case err := <-done:
				return err
			case <-ctx.Done():
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					return ErrGatewayTimeout()
				}
				return ctx.Err()
			}
		}
	}
}
