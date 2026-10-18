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