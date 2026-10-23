package repository

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
)

const maxContentSize = 1048576 // 1 MB

// ValidateContentSize checks if the content exceeds the maximum size.
// The limit is measured in BYTES (not runes): it bounds memory, storage and
// wire size, and matches octet_length(content) in migrations/0001_init.up.sql
// and BodyLimit in cmd/server/main.go (docs/FIXES.md #8).
func ValidateContentSize(content string) error {
	if len(content) > maxContentSize {
		return fmt.Errorf("content exceeds %d bytes (1 MB) limit", maxContentSize)
	}
	return nil
}

// ValidateTitle checks if the title is valid (length in runes, not bytes).
func ValidateTitle(title string) error {
	if utf8.RuneCountInString(strings.TrimSpace(title)) == 0 {
		return fmt.Errorf("title is required")
	}
	if utf8.RuneCountInString(title) > 500 {
		return fmt.Errorf("title exceeds 500 characters")
	}
	return nil
}

// ValidateQuestion checks if the question is valid (length in runes, not bytes).
func ValidateQuestion(question string) error {
	if utf8.RuneCountInString(strings.TrimSpace(question)) == 0 {
		return fmt.Errorf("question is required")
	}
	if utf8.RuneCountInString(question) > 4000 {
		return fmt.Errorf("question exceeds 4000 characters")
	}
	return nil
}

// EnsureConnection verifies the database connection is healthy.
func EnsureConnection(ctx context.Context, pool *pgxpool.Pool) error {
	return pool.Ping(ctx)
}
