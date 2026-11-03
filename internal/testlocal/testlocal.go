// Package testlocal gates the tests that need what the repository does not
// hold — a local game archive under var/, a reference binary, an external
// sample — or that only report and never fail. Such a test is named
// TestLocal…, lives in a local_*_test.go file, and runs only when its
// environment variable is set; `make test` and CI see it skipped.
//
// The package imports nothing of this module, so any package's tests can use
// it without an import cycle.
package testlocal

import (
	"os"
	"strconv"
	"testing"
)

// Enabled skips t unless the environment variable name is set to 1. purpose
// completes the skip message "set NAME=1 to …".
func Enabled(t testing.TB, name, purpose string) {
	t.Helper()
	if os.Getenv(name) != "1" {
		t.Skipf("set %s=1 to %s", name, purpose)
	}
}

// Value returns the environment variable name and skips t when it is empty.
// what completes the skip message "set NAME to …".
func Value(t testing.TB, name, what string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Skipf("set %s to %s", name, what)
	}
	return value
}

// Int returns the environment variable name as a positive integer, or
// fallback when it is unset, so a local test's window can be widened from the
// environment. A value that is not a positive integer fails t.
func Int(t testing.TB, name string, fallback int) int {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		t.Fatalf("invalid %s %q", name, value)
	}
	return parsed
}
