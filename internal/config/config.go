// Package config reads process environment variables.
package config

import "os"

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
