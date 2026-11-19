package rag

import (
	"strings"
	"testing"
)

func TestChunkerEmpty(t *testing.T) {
	c := NewChunker()
	if chunks := c.Chunk("   "); chunks != nil {
		t.Errorf("empty content: want nil, got %v", chunks)
	}
}

func TestChunkerShortContent(t *testing.T) {
	c := NewChunker()
	chunks := c.Chunk("Короткий текст")
	if len(chunks) != 1 || chunks[0] != "Короткий текст" {
		t.Fatalf("want one chunk in full, got %v", chunks)
	}
}

func TestChunkerLongContent(t *testing.T) {
	c := NewChunker()
	content := strings.Repeat("abcdefghij ", 2000) // 22 000 characters

	chunks := c.Chunk(content)
	if len(chunks) < 2 {
		t.Fatalf("long content: want multiple chunks, got %d", len(chunks))
	}
	for i, ch := range chunks {
		if strings.TrimSpace(ch) == "" {
			t.Errorf("chunk %d is empty", i)
		}
	}
}

func TestChunkerUnicodeBoundaries(t *testing.T) {
	c := NewChunker()
	// Multi-line Russian text longer than chunk size.
	line := strings.Repeat("абракадабра ", 100) // 1300 characters
	content := strings.Repeat(line+"\n", 3)

	chunks := c.Chunk(content)
	if len(chunks) < 3 {
		t.Fatalf("want >= 3 chunks, got %d", len(chunks))
	}
	// No broken UTF-8 sequences should be present.
	for i, ch := range chunks {
		if !strings.Contains(ch, "абракадабра") {
			t.Errorf("chunk %d lost content: %q", i, ch)
		}
	}
}
