// Package config reads process environment variables.
package config

import (
	"os"
	"time"
)

// String returns the value of the environment variable key, or def when the
// variable is unset or empty. It is the single shared implementation; the
// original getEnvDefault was duplicated in cmd/server/main.go and
// internal/rag/ollama.go (docs/FIXES.md #15).
func String(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Duration returns the value of the environment variable key parsed as a
// time.Duration (e.g. "90s", "2m"), or def when the variable is unset, empty
// or not a valid duration (docs/TODO_SIGNIFICANT.md SIG-4).
func Duration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return def
	}
	return d
}
