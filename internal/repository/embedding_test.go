package repository

import (
	"strings"
	"testing"
)

func TestValidateTitle(t *testing.T) {
	if err := ValidateTitle("CVE-2021-44228"); err != nil {
		t.Errorf("валидный title: %v", err)
	}
	if err := ValidateTitle(""); err == nil {
		t.Error("пустой title должен быть ошибкой")
	}
	if err := ValidateTitle("   "); err == nil {
		t.Error("title из пробелов должен быть ошибкой")
	}
	if err := ValidateTitle(strings.Repeat("т", 501)); err == nil {
		t.Error("title длиннее 500 символов должен быть ошибкой")
	}
}

func TestValidateContentSize(t *testing.T) {
	if err := ValidateContentSize("some content"); err != nil {
		t.Errorf("валидный content: %v", err)
	}
	if err := ValidateContentSize(strings.Repeat("a", 2*1024*1024)); err == nil {
		t.Error("content больше 1 МБ должен быть ошибкой")
	}
}

func TestValidateQuestion(t *testing.T) {
	if err := ValidateQuestion("что такое Log4Shell?"); err != nil {
		t.Errorf("валидный question: %v", err)
	}
	if err := ValidateQuestion("   "); err == nil {
		t.Error("question из пробелов должен быть ошибкой")
	}
	if err := ValidateQuestion(strings.Repeat("а", 4001)); err == nil {
		t.Error("question длиннее 4000 символов должен быть ошибкой")
	}
}

// TestValidateLengthsInRunes verifies that title and question limits are
// measured in Unicode runes (not bytes), per docs/FIXES.md #8.
func TestValidateLengthsInRunes(t *testing.T) {
	// 500 Cyrillic chars = 1000 bytes — must be valid (exactly at the rune limit).
	title500 := strings.Repeat("т", 500)
	if err := ValidateTitle(title500); err != nil {
		t.Errorf("title из 500 кириллических символов (1000 байт) должен быть валиден: %v", err)
	}

	// 501 Cyrillic chars = 1002 bytes — must be rejected (exceeds rune limit).
	title501 := strings.Repeat("т", 501)
	if err := ValidateTitle(title501); err == nil {
		t.Error("title из 501 кириллического символа должен быть отклонён")
	}

	// 4000 Cyrillic chars = 8000 bytes — must be valid (exactly at the rune limit).
	q4000 := strings.Repeat("а", 4000)
	if err := ValidateQuestion(q4000); err != nil {
		t.Errorf("question из 4000 кириллических символов (8000 байт) должен быть валиден: %v", err)
	}

	// 4001 Cyrillic chars = 8002 bytes — must be rejected (exceeds rune limit).
	q4001 := strings.Repeat("а", 4001)
	if err := ValidateQuestion(q4001); err == nil {
		t.Error("question из 4001 кириллического символа должен быть отклонён")
	}

	// Content: size limit is in BYTES (not runes).
	// 1 048 576 bytes — must be valid (exactly at the byte limit).
	content1MB := strings.Repeat("a", 1048576)
	if err := ValidateContentSize(content1MB); err != nil {
		t.Errorf("content из 1 048 576 байт должен быть валиден: %v", err)
	}

	// 1 048 577 bytes — must be rejected.
	contentOver := strings.Repeat("a", 1048577)
	if err := ValidateContentSize(contentOver); err == nil {
		t.Error("content из 1 048 577 байт должен быть отклонён")
	}
}