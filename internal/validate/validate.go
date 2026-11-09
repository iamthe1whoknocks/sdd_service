// Package validate holds the request payload rules of docs/spec.md §4.
//
// It is the single source of truth for input validation: the limits live here,
// not in the HTTP handlers and not in the database repository
// Transport layers map Error.TooLarge to 413 and any
// other violation to 422 without re-implementing the rules.
package validate

import (
	"strings"
	"unicode/utf8"
)

// Limits from docs/spec.md §4 and api/openapi.yaml.
const (
	// MaxContentBytes bounds document content in BYTES: the limit protects
	// memory, storage and the wire. It matches octet_length(content) in
	// migrations/0001_init.up.sql and BodyLimit in cmd/server/main.go.
	MaxContentBytes = 1 << 20 // 1 MB
	// MaxTitleRunes and MaxQuestionRunes bound text in RUNES (Unicode code
	// points), which is what OpenAPI maxLength and the web UI counter use
	// (500 and 4 000 respectively).
	MaxTitleRunes    = 500
	MaxQuestionRunes = 4000
)

// Error is a payload rule violation. Message is safe to return to the client.
type Error struct {
	// TooLarge marks a violation of the byte limit (HTTP 413). Any other
	// violation is a malformed payload (HTTP 422).
	TooLarge bool
	Message  string
}

func (e *Error) Error() string { return e.Message }

// Question checks that the question is present and not longer than
// MaxQuestionRunes. Surrounding whitespace is ignored for both checks.
func Question(question string) error {
	q := strings.TrimSpace(question)
	if utf8.RuneCountInString(q) == 0 {
		return &Error{Message: "question is required"}
	}
	if utf8.RuneCountInString(q) > MaxQuestionRunes {
		return &Error{Message: "question exceeds 4000 characters"}
	}
	return nil
}

// Title checks that the title is present and not longer than MaxTitleRunes.
func Title(title string) error {
	t := strings.TrimSpace(title)
	if utf8.RuneCountInString(t) == 0 {
		return &Error{Message: "title is required"}
	}
	if utf8.RuneCountInString(t) > MaxTitleRunes {
		return &Error{Message: "title exceeds 500 characters"}
	}
	return nil
}

// Content checks that the content is present and fits into MaxContentBytes.
// The size is measured in bytes on purpose (see MaxContentBytes).
func Content(content string) error {
	if len(content) > MaxContentBytes {
		return &Error{TooLarge: true, Message: "content exceeds 1 MB limit"}
	}
	if strings.TrimSpace(content) == "" {
		return &Error{Message: "content is required"}
	}
	return nil
}
