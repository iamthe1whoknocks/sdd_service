package rag

import (
	"strings"
	"unicode"
	"unicode/utf8"
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

	runes := []rune(content)
	total := len(runes)

	// If content is smaller than chunk size, return as-is (rune-based,
	// so multi-byte text is handled consistently).
	if total <= c.ChunkSize {
		return []string{content}
	}

	var chunks []string
	var pending string // accumulate tiny chunks to merge with neighbors

	// Boundary-driven stride: each next chunk starts Overlap characters
	// before the end of the current one.
	for start := 0; start < total; {
		end := start + c.ChunkSize
		if end > total {
			end = total
		}

		// Try to break at a word (or sentence) boundary.
		chunkEnd := findWordBoundary(runes, start, end)
		chunk := string(runes[start:chunkEnd])

		runeCount := utf8.RuneCountInString(chunk)

		// Merge tiny chunks with pending or the next large chunk.
		if runeCount < MinChunkSize {
			pending += chunk

			if chunkEnd >= total {
				// End of document: append pending to the last chunk.
				if len(chunks) > 0 {
					chunks[len(chunks)-1] += pending
				}
				break
			}

			// Overlap the tail, but always move forward to avoid infinite loop.
			nextStart := chunkEnd - c.Overlap
			if nextStart <= start {
				nextStart = chunkEnd
			}
			start = nextStart
			continue
		}

		// Attach any pending tiny chunks to this large chunk.
		if pending != "" {
			chunk = pending + chunk
			pending = ""
		}

		chunks = append(chunks, chunk)

		if chunkEnd >= total {
			break
		}

		// Overlap the tail of the current chunk, but always move forward:
		// a boundary very close to start must not push us backwards
		// (which would cause an infinite loop).
		nextStart := chunkEnd - c.Overlap
		if nextStart <= start {
			nextStart = chunkEnd
		}
		start = nextStart
	}

	// Flush any remaining pending chunks to the last large chunk.
	if pending != "" && len(chunks) > 0 {
		chunks[len(chunks)-1] += pending
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
