package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	echoMiddleware "github.com/labstack/echo/v4/middleware"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"vulnrag/internal/httperr"
	"vulnrag/internal/middleware"
)

// NewHTTPErrorHandler returns an Echo HTTP error handler that logs the full
// error and replies with a generic message to the client.
//
// The handler is shared by prod and tests so that error formatting is
// identical regardless of the router used.
func NewHTTPErrorHandler(log *zap.Logger) echo.HTTPErrorHandler {
	return func(err error, c echo.Context) {
		logErr := func(lvl zapcore.Level, msg string, status int) {
			if ce := log.Check(lvl, msg); ce != nil {
				ce.Write(
					zap.String("request_id", requestID(c)),
					zap.String("method", c.Request().Method),
					zap.String("path", c.Request().URL.Path),
					zap.Int("status", status),
					zap.Error(err),
				)
			}
		}

		he, ok := err.(*echo.HTTPError)
		if !ok {
			logErr(zapcore.ErrorLevel, "unhandled handler error", 500)
			_ = httperr.Write(c, 500, "internal_error", "internal server error")
			return
		}

		if he.Code >= 500 {
			logErr(zapcore.ErrorLevel, "handler error", he.Code)
			_ = httperr.Write(c, he.Code, "internal_error", "internal server error")
			return
		}

		// 4xx: safe to expose the status text (e.g. "Not Found", "Unprocessable
		// Entity").  Sanitize any user-supplied message to prevent log forging
		// and HTML injection.
		logErr(zapcore.InfoLevel, "request rejected", he.Code)
		detail := strings.TrimSpace(http.StatusText(he.Code))
		if msg, ok := he.Message.(string); ok && strings.TrimSpace(msg) != "" {
			detail = msg
		}
		_ = httperr.Write(c, he.Code, "http_error", detail)
	}
}

// middlewareRegistrar wraps *echo.Echo to the generated EchoRouter interface
// and applies route-specific middleware.  All routes are
// registered through the generated RegisterHandlers, so prod and tests share
// the same path definitions from api/openapi.yaml.
type middlewareRegistrar struct {
	*echo.Echo
	askTimeout       time.Duration
	documentsTimeout time.Duration
}

func (r middlewareRegistrar) POST(path string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) *echo.Route {
	switch path {
	case "/api/v1/ask":
		m = append(m, middleware.Timeout(r.askTimeout))
	case "/api/v1/documents":
		m = append(m, middleware.Timeout(r.documentsTimeout))
	}
	return r.Echo.POST(path, h, m...)
}

// Server bundles the endpoint handlers into a single ServerInterface so that
// both prod and tests register the same routes through the generated
// RegisterHandlers function.
type Server struct {
	*AskHandler
	*DocumentsHandler
}

func (s *Server) GetHealth(c echo.Context) error {
	return c.JSON(200, map[string]string{"status": "ok"})
}

// RouterConfig holds the HTTP pipeline parameters shared by prod and tests.
type RouterConfig struct {
	AskTimeout       time.Duration // deadline for POST /api/v1/ask (60s by spec)
	DocumentsTimeout time.Duration // deadline for POST /api/v1/documents
}

// NewRouter creates a fully configured Echo router.  The same function is
// called from cmd/server/main.go (prod) and handler_test.go (tests) so that
// route definitions and middleware ordering are identical.
func NewRouter(si ServerInterface, cfg RouterConfig, log *zap.Logger) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.HTTPErrorHandler = NewHTTPErrorHandler(log)

	// Global middleware (request ID, logging, panic recovery, body limit).
	e.Use(middleware.RequestID())
	e.Use(middleware.Logger(log))
	e.Use(middleware.Recover(log))
	e.Use(echoMiddleware.BodyLimit("1MB"))

	// Register routes from api/openapi.yaml.  The registrar adds the
	// per-route timeout middleware on POST /api/v1/ask (60s by spec) and
	// POST /api/v1/documents so that
	// /health stays unaffected.
	reg := middlewareRegistrar{Echo: e, askTimeout: cfg.AskTimeout, documentsTimeout: cfg.DocumentsTimeout}
	RegisterHandlers(reg, si)

	return e
}
