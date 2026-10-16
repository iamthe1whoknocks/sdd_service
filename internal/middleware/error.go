package middleware

import "fmt"

type ErrorResponse struct {
	Detail string `json:"detail,omitempty"`
	Type   string `json:"type,omitempty"`
}

func (e *ErrorResponse) Error() string {
	return fmt.Sprintf("code:%d, message %s", e.Type, e.Detail)
}
