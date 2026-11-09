package validate

import (
	"errors"
	"strings"
	"testing"
)

func TestTitle(t *testing.T) {
	if err := Title("CVE-2021-44228"); err != nil {
		t.Errorf("valid title rejected: %v", err)
	}
	if err := Title(""); err == nil {
		t.Error("empty title must be an error")
	}
	if err := Title("   "); err == nil {
		t.Error("whitespace-only title must be an error")
	}
	if err := Title(strings.Repeat("т", 501)); err == nil {
		t.Error("title longer than 500 characters must be an error")
	}
}

func TestContent(t *testing.T) {
	if err := Content("some content"); err != nil {
		t.Errorf("valid content rejected: %v", err)
	}
	if err := Content(strings.Repeat("a", 2*1024*1024)); err == nil {
		t.Error("content larger than 1 MB must be an error")
	}
	if err := Content("   "); err == nil {
		t.Error("whitespace-only content must be an error")
	}
}

func TestQuestion(t *testing.T) {
	if err := Question("что такое Log4Shell?"); err != nil {
		t.Errorf("valid question rejected: %v", err)
	}
	if err := Question("   "); err == nil {
		t.Error("whitespace-only question must be an error")
	}
	if err := Question(strings.Repeat("а", 4001)); err == nil {
		t.Error("question longer than 4000 characters must be an error")
	}
}

// TestLengthsInRunes verifies that the title and question limits are measured
// in Unicode runes rather than bytes.
func TestLengthsInRunes(t *testing.T) {
	// 500 Cyrillic characters = 1000 bytes — valid, exactly at the rune limit.
	if err := Title(strings.Repeat("т", 500)); err != nil {
		t.Errorf("title of 500 Cyrillic characters (1000 bytes) must be valid: %v", err)
	}

	// 501 Cyrillic characters — rejected, over the rune limit.
	if err := Title(strings.Repeat("т", 501)); err == nil {
		t.Error("title of 501 Cyrillic characters must be rejected")
	}

	// 4000 Cyrillic characters = 8000 bytes — valid, exactly at the rune limit.
	if err := Question(strings.Repeat("а", 4000)); err != nil {
		t.Errorf("question of 4000 Cyrillic characters (8000 bytes) must be valid: %v", err)
	}

	// 4001 Cyrillic characters — rejected.
	if err := Question(strings.Repeat("а", 4001)); err == nil {
		t.Error("question of 4001 Cyrillic characters must be rejected")
	}
}

// TestContentSizeInBytes verifies that the content limit is measured in BYTES
// (it bounds storage and wire size), not in runes.
func TestContentSizeInBytes(t *testing.T) {
	if err := Content(strings.Repeat("a", MaxContentBytes)); err != nil {
		t.Errorf("content of exactly %d bytes must be valid: %v", MaxContentBytes, err)
	}
	if err := Content(strings.Repeat("a", MaxContentBytes+1)); err == nil {
		t.Error("content of 1 048 577 bytes must be rejected")
	}
}

// TestContentErrorMapsTo413 checks that only the byte-limit violation is
// flagged as TooLarge, so the transport layer can answer 413 instead of 422.
func TestContentErrorMapsTo413(t *testing.T) {
	var ve *Error
	if err := Content(strings.Repeat("a", MaxContentBytes+1)); err == nil {
		t.Fatal("oversized content must be rejected")
	} else if !errors.As(err, &ve) || !ve.TooLarge {
		t.Errorf("oversized content must be flagged TooLarge, got %#v", err)
	}

	if err := Content(""); err == nil {
		t.Fatal("empty content must be rejected")
	} else if errors.As(err, &ve) && ve.TooLarge {
		t.Errorf("empty content must not be flagged TooLarge, got %#v", err)
	}
}
