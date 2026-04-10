// Package testutil provides minimal test helper utilities for the tsq test suite.
package testutil

import (
	"errors"
	"strings"
	"testing"
)

// Equal fails the test if got != want.
func Equal(t *testing.T, got, want interface{}) {
	t.Helper()
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

// ErrorIs fails the test if !errors.Is(got, want).
func ErrorIs(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Errorf("got error %v, want %v", got, want)
	}
}

// NilError fails the test if err != nil.
func NilError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Contains fails the test if s does not contain substr.
func Contains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("string %q does not contain %q", s, substr)
	}
}

// Failf fails the test with a formatted message.
func Failf(t *testing.T, format string, args ...interface{}) {
	t.Helper()
	t.Fatalf(format, args...)
}
