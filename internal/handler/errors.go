package handler

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
)

// ErrResponse builds a RFC 7807-style error response.
func ErrResponse(code int, etype, detail string) echo.HandlerFunc {
	return func(c echo.Context) error {
		errResp := Error{
			Error: struct {
				Detail *string `json:"detail,omitempty"`
				Type   *string `json:"type,omitempty"`
			}{
				Type:   &etype,
				Detail: &detail,
			},
		}
		return c.JSON(code, errResp)
	}
}

// ValidationErr returns a 422 Unprocessable Entity error.
func ValidationErr(detail string) error {
	msg := fmt.Sprintf("validation error: %s", detail)
	return &echo.HTTPError{
		Code:    http.StatusUnprocessableEntity,
		Message: msg,
	}
}

// ContentTooLargeErr returns a 413 Payload Too Large error.
func ContentTooLargeErr() error {
	msg := "payload too large: content exceeds 1 MB limit"
	return &echo.HTTPError{
		Code:    http.StatusRequestEntityTooLarge,
		Message: msg,
	}
}
