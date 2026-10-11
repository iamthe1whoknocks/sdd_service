package rag

import (
	"strings"
	"unicode"
)

const (
	// DefaultChunkSize is the target size for each chunk in characters.
	DefaultChunkSize = 500
	// DefaultOverlap is the overlap between consecutive chunks in characters.
	DefaultOverlap = 100
	// MinChunkSize is the minimum chunk size to avoid tiny chunks.
	MinChunkSize = 50
)

// Chunker splits document content into overlapping chunks.
type Chunker struct {
	ChunkSize int
	Overlap   int
}

// NewChunker creates a new Chunker with default settings.
func NewChunker() *Chunker {
	return &Chunker{
		ChunkSize: DefaultChunkSize,
		Overlap:   DefaultOverlap,
	}
}

// Chunk splits content into overlapping chunks.
func (c *Chunker) Chunk(content string) []string {
	content = strings.TrimSpace(content)
	if len(content) == 0 {
		return nil
	}

	// If content is smaller than chunk size, return as-is
	if len(content) <= c.ChunkSize {
		return []string{content}
	}

	var chunks []string
	runes := []rune(content)
	total := len(runes)

	for start := 0; start < total; start += c.ChunkSize - c.Overlap {
		end := start + c.ChunkSize
		if end > total {
			end = total
		}

		// Try to break at word boundary
		chunkEnd := findWordBoundary(runes, start, end)
		chunk := string(runes[start:chunkEnd])

		// Skip tiny chunks
		if len([]rune(chunk)) >= MinChunkSize {
			chunks = append(chunks, chunk)
		}

		start = chunkEnd - c.Overlap
		if start < chunkEnd {
			start = chunkEnd
		}

		if start >= total {
			break
		}
	}

	return chunks
}

// findWordBoundary finds the best word boundary before the target position.
func findWordBoundary(runes []rune, start, target int) int {
	end := target
	if end > len(runes) {
		end = len(runes)
	}

	// Try to find a sentence boundary first (.!?)
	for i := end - 1; i > start; i-- {
		if runes[i] == '.' || runes[i] == '!' || runes[i] == '?' {
			// Include the punctuation
			return i + 1
		}
	}

	// Try to find a whitespace boundary
	for i := end - 1; i > start; i-- {
		if unicode.IsSpace(runes[i]) {
			return i + 1
		}
	}

	// No boundary found, use target position
	return end
}
