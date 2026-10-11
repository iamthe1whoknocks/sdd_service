package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const maxContentSize = 1048576 // 1 MB

// ValidateContentSize checks if the content exceeds the maximum size.
func ValidateContentSize(content string) error {
	if len(content) > maxContentSize {
		return fmt.Errorf("content exceeds %d bytes (1 MB) limit", maxContentSize)
	}
	return nil
}

// ValidateTitle checks if the title is valid.
func ValidateTitle(title string) error {
	if len(strings.TrimSpace(title)) == 0 {
		return fmt.Errorf("title is required")
	}
	if len(title) > 500 {
		return fmt.Errorf("title exceeds 500 characters")
	}
	return nil
}

// ValidateQuestion checks if the question is valid.
func ValidateQuestion(question string) error {
	if len(strings.TrimSpace(question)) == 0 {
		return fmt.Errorf("question is required")
	}
	if len(question) > 4000 {
		return fmt.Errorf("question exceeds 4000 characters")
	}
	return nil
}

// EnsureConnection verifies the database connection is healthy.
func EnsureConnection(ctx context.Context, pool *pgxpool.Pool) error {
	return pool.Ping(ctx)
}
