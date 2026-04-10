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
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Errorf("stderr = %q, want 'unknown command'", stderr.String())
	}
}

func TestNoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Errorf("stderr = %q, want usage info", stderr.String())
	}
}

func TestQueryMissingArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"query"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "QUERY_FILE") {
		t.Errorf("stderr = %q, want mention of QUERY_FILE", stderr.String())
	}
}

func TestCheckMissingArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"check"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
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
