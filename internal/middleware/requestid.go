package middleware

import (
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

const (
	// requestIDKey is the echo context key that carries the request ID to the
	// logger and the handlers.
	requestIDKey = "requestID"

	// maxRequestIDLen bounds the length of an inbound, client-supplied ID. It
	// is generous enough for UUIDs, ULIDs, snowflakes and W3C trace IDs while
	// keeping untrusted input away from the log volume.
	maxRequestIDLen = 128
)

// RequestID propagates the inbound X-Request-ID when the value is safe to use
// and otherwise generates a fresh UUID. The resulting ID is written to the
// response header and to the echo context for downstream logging, so a client
// that correlates its own request with server logs sees the same ID on both
// sides (docs/spec.md §9, docs/FIXES.md #17).
//
// An inbound ID is untrusted input: it reaches the access log and is reflected
// back in a response header, so only a conservative allowlist of characters is
// accepted. Anything else (empty, too long, control characters, whitespace,
// markup) falls back to a generated UUID rather than a rejection — a malformed
// trace header must not break an otherwise valid request.
func RequestID() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			id := sanitizeRequestID(c.Request().Header.Get(echo.HeaderXRequestID))
			if id == "" {
				id = uuid.New().String()
			}

			// Set the header before calling next: a handler or an earlier
			// middleware may commit the response, and the ID has to be present
			// in that case as well.
			c.Response().Header().Set(echo.HeaderXRequestID, id)
			c.Set(requestIDKey, id)

			return next(c)
		}
	}
}

// sanitizeRequestID returns id when it fits the allowlist, or an empty string
// if it must be replaced with a generated one. The length bound is counted in
// bytes, which is what matters for the log volume; non-ASCII bytes are rejected
// by the allowlist anyway, so a multi-byte value never survives this check.
func sanitizeRequestID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > maxRequestIDLen {
		return ""
	}

	for i := 0; i < len(id); i++ {
		if !isRequestIDByte(id[i]) {
			return ""
		}
	}

	return id
}

// isRequestIDByte reports whether b is safe to log and to reflect back in a
// header: alphanumerics plus "-", "_", "." and ":". Everything else is
// rejected — control characters (log forging), whitespace, and the markup
// characters "<>\"' that would be echoed into a response header.
func isRequestIDByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z',
		b >= 'A' && b <= 'Z',
		b >= '0' && b <= '9':
		return true
	case b == '-', b == '_', b == '.', b == ':':
		return true
	default:
		return false
	}
}
