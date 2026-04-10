package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "tsq version") {
		t.Errorf("stdout = %q, want 'tsq version ...'", stdout.String())
	}
}

func TestUnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"bogus"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Errorf("stderr = %q, want 'unknown command'", stderr.String())
	}
}

func TestNoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Errorf("stderr = %q, want usage info", stderr.String())
	}
}

func TestQueryMissingArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"query"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "QUERY_FILE") {
		t.Errorf("stderr = %q, want mention of QUERY_FILE", stderr.String())
	}
}

func TestCheckMissingArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"check"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "QUERY_FILE") {
		t.Errorf("stderr = %q, want mention of QUERY_FILE", stderr.String())
	}
}

func TestQueryBadFormat(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"query", "--format", "xml", "test.ql"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "unknown format") {
		t.Errorf("stderr = %q, want 'unknown format'", stderr.String())
	}
}

func TestCheckNonexistentFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"check", "/nonexistent/file.ql"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "error:") {
		t.Errorf("stderr = %q, want error message", stderr.String())
	}
}

// Regression: global flags only should print usage, not "unknown command --verbose".
func TestGlobalFlagsOnly(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--verbose"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if strings.Contains(stderr.String(), "unknown command") {
		t.Errorf("stderr should show usage, not 'unknown command'; got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Errorf("stderr = %q, want usage info", stderr.String())
	}
}

func TestGlobalFlagsMultiple(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--verbose", "--quiet"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Errorf("stderr = %q, want usage info", stderr.String())
	}
}

// Regression: -h/--help should return exit code 0 for all subcommands.
func TestHelpExitCode(t *testing.T) {
	for _, args := range [][]string{
		{"extract", "-h"},
		{"extract", "--help"},
		{"query", "-h"},
		{"query", "--help"},
		{"check", "-h"},
		{"check", "--help"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(args, &stdout, &stderr)
			if code != 0 {
				t.Errorf("exit code = %d, want 0 for %v", code, args)
			}
		})
	}
}

// Regression: usage errors return exit code 2, runtime errors return 1.
func TestUsageErrorExitCode(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"no args", []string{}, 2},
		{"unknown command", []string{"bogus"}, 2},
		{"global flags only", []string{"--verbose"}, 2},
		{"query missing file", []string{"query"}, 2},
		{"check missing file", []string{"check"}, 2},
		// Runtime errors should be 1.
		{"check nonexistent file", []string{"check", "/nonexistent/file.ql"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)
			if code != tt.want {
				t.Errorf("exit code = %d, want %d; stderr: %s", code, tt.want, stderr.String())
			}
		})
	}
}

// Regression: global flags followed by a valid subcommand should work.
func TestGlobalFlagsBeforeSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--verbose", "version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "tsq version") {
		t.Errorf("stdout = %q, want 'tsq version ...'", stdout.String())
	}
}
