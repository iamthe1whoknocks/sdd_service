package rag

import (
	"strings"
	"testing"
)

func TestChunkerEmpty(t *testing.T) {
	c := NewChunker()
	if chunks := c.Chunk("   "); chunks != nil {
		t.Errorf("пустой контент: want nil, got %v", chunks)
	}
}

func TestChunkerShortContent(t *testing.T) {
	c := NewChunker()
	chunks := c.Chunk("Короткий текст")
	if len(chunks) != 1 || chunks[0] != "Короткий текст" {
		t.Fatalf("want один чанк целиком, got %v", chunks)
	}
}

func TestChunkerLongContent(t *testing.T) {
	c := NewChunker()
	content := strings.Repeat("abcdefghij ", 2000) // 22 000 символов

	chunks := c.Chunk(content)
	if len(chunks) < 2 {
		t.Fatalf("длинный контент: want несколько чанков, got %d", len(chunks))
	}
	for i, ch := range chunks {
		if strings.TrimSpace(ch) == "" {
			t.Errorf("чанк %d пустой", i)
		}
	}
}

func TestChunkerUnicodeBoundaries(t *testing.T) {
	c := NewChunker()
	// Многострочный русский текст длиннее размера чанка.
	line := strings.Repeat("абракадабра ", 100) // 1300 символов
	content := strings.Repeat(line+"\n", 3)

	chunks := c.Chunk(content)
	if len(chunks) < 3 {
		t.Fatalf("want >= 3 чанка, got %d", len(chunks))
	}
	// Не должно быть битых UTF-8 последовательностей.
	for i, ch := range chunks {
		if !strings.Contains(ch, "абракадабра") {
			t.Errorf("чанк %d потерял содержимое: %q", i, ch)
		}
	}
}