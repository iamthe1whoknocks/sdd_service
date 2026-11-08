package config

import (
	"os"
	"testing"
	"time"
)

func TestDurationReturnsDefaultWhenEmpty(t *testing.T) {
	os.Unsetenv("TEST_KEY")
	got := Duration("TEST_KEY", 42*time.Second)
	if got != 42*time.Second {
		t.Errorf("Duration() = %v, want 42s", got)
	}
}

func TestDurationReturnsDefaultWhenEmptyString(t *testing.T) {
	os.Setenv("TEST_KEY", "")
	defer os.Unsetenv("TEST_KEY")

	got := Duration("TEST_KEY", 1*time.Minute)
	if got != 1*time.Minute {
		t.Errorf("Duration() = %v, want 1m", got)
	}
}

func TestDurationParsesValidValue(t *testing.T) {
	os.Setenv("TEST_KEY", "90s")
	defer os.Unsetenv("TEST_KEY")

	got := Duration("TEST_KEY", 5*time.Minute)
	if got != 90*time.Second {
		t.Errorf("Duration() = %v, want 90s", got)
	}
}

func TestDurationParsesMinutes(t *testing.T) {
	os.Setenv("TEST_KEY", "2m")
	defer os.Unsetenv("TEST_KEY")

	got := Duration("TEST_KEY", 10*time.Second)
	if got != 2*time.Minute {
		t.Errorf("Duration() = %v, want 2m", got)
	}
}

func TestDurationReturnsDefaultOnInvalidValue(t *testing.T) {
	os.Setenv("TEST_KEY", "not-a-duration")
	defer os.Unsetenv("TEST_KEY")

	got := Duration("TEST_KEY", 10*time.Second)
	if got != 10*time.Second {
		t.Errorf("Duration() = %v, want 10s (fallback on parse error)", got)
	}
}

func TestDurationReturnsDefaultOnNegativeValue(t *testing.T) {
	os.Setenv("TEST_KEY", "-5s")
	defer os.Unsetenv("TEST_KEY")

	got := Duration("TEST_KEY", 10*time.Second)
	if got != 10*time.Second {
		t.Errorf("Duration() = %v, want 10s (fallback on negative)", got)
	}
}
