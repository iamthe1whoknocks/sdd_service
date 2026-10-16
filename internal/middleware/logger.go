package middleware

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// zapAdapter реализует интерфейс echo.Logger, делегируя вызовы zap.
type zapAdapter struct {
	logger *zap.Logger
}

func (z *zapAdapter) Start() error                          { return nil }
func (z *zapAdapter) Stop() error                           { return nil }
func (z *zapAdapter) Info(msg string, args ...interface{})  { z.logger.Sugar().Infof(msg, args...) }
func (z *zapAdapter) Warn(msg string, args ...interface{})  { z.logger.Sugar().Warnf(msg, args...) }
func (z *zapAdapter) Error(msg string, args ...interface{}) { z.logger.Sugar().Errorf(msg, args...) }
func (z *zapAdapter) Fatal(msg string, args ...interface{}) { z.logger.Sugar().Fatalf(msg, args...) }
func (z *zapAdapter) Debug(msg string, args ...interface{}) { z.logger.Sugar().Debugf(msg, args...) }
func (z *zapAdapter) Print(msg string, args ...interface{}) { z.logger.Sugar().Infof(msg, args...) }

// Logger logs each request with method, path, status, duration, and request ID.
func Logger(logger *zap.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()

			err := next(c)

			duration := time.Since(start)
			requestID := c.Get("requestID")

			status := c.Response().Status
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
					_ = c.JSON(http.StatusInternalServerError, map[string]string{
						"type":   "500",
						"detail": "internal server_error",
					})
				}
			}()
			return next(c)
		}
	}
}
