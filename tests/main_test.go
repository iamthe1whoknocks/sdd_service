package tests

import (
	"testing"
)

func TestMain(m *testing.M) {
	exitCode := Run()
	if exitCode == 0 {
		m.Run()
	}
}
