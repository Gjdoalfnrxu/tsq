// Package integration_test provides end-to-end CLI integration tests that build
// the tsq binary and invoke it as a subprocess, exercising the full pipeline
// from extraction through query evaluation.
//
// These tests catch gaps that library-level tests miss — notably the system-rules
// injection path (which must flow through cmd/tsq/main.go's compileAndEval) and
// the exact CLI flag/output contract.
//
// The tests require CGO_ENABLED=1 (for tree-sitter) and a working Go toolchain.
// They are skipped automatically if the binary cannot be compiled.
package integration_test

import (
	"bytes"
	"encoding/csv"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// buildTSQBinary compiles the tsq binary into a temporary directory and returns
// its path. The test is skipped if go build fails (e.g. missing CGO toolchain).
func buildTSQBinary(t *testing.T) string {
	t.Helper()

	// Locate repo root relative to this file.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Dir(thisFile)

	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "tsq")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/tsq")
	cmd.Dir = repoRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Skipf("skipping CLI integration test: could not build tsq binary: %v\n%s", err, stderr.String())
	}
	return binPath
}

// cliRepoRoot returns the absolute path to the repository root.
func cliRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(thisFile)
}

// runExtract runs "tsq extract --dir <dir> --output <db>" and returns stderr.
// Fails the test on non-zero exit.
func runExtract(t *testing.T, tsq, dir, dbFile string) {
	t.Helper()
	cmd := exec.Command(tsq, "extract", "--dir", dir, "--output", dbFile)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("tsq extract --dir %s failed: %v\nstderr: %s", dir, err, stderr.String())
	}
	if _, err := os.Stat(dbFile); err != nil {
		t.Fatalf("extract did not produce output file %s: %v", dbFile, err)
	}
}

// runCLIQuery runs "tsq query --db <db> --format <fmt> <queryFile>" and returns stdout.
// Fails the test on non-zero exit.
func runCLIQuery(t *testing.T, tsq, dbFile, format, queryFile string) string {
	t.Helper()
	cmd := exec.Command(tsq, "query", "--db", dbFile, "--format", format, queryFile)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("tsq query --db %s --format %s %s failed: %v\nstderr: %s\nstdout: %s",
			dbFile, format, queryFile, err, stderr.String(), stdout.String())
	}
	return stdout.String()
}

// writeQueryFile writes a QL query string to a temp file and returns its path.
func writeQueryFile(t *testing.T, dir, name, src string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write query file %s: %v", path, err)
	}
	return path
}

// parseCSV parses CSV output, returning all rows (header first).
func parseCSV(t *testing.T, raw string) [][]string {
	t.Helper()
	rows, err := csv.NewReader(strings.NewReader(raw)).ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v\nraw: %q", err, raw)
	}
	return rows
}

// TestCLI_ExtractAndQuery_Functions verifies that tsq extract + tsq query works
// end-to-end for a simple function-listing query. This is the baseline smoke
// test: no system rules needed, just extraction and QL evaluation.
func TestCLI_ExtractAndQuery_Functions(t *testing.T) {
	tsq := buildTSQBinary(t)
	root := cliRepoRoot(t)
	workDir := t.TempDir()

	// Extract the simple project.
	dbFile := filepath.Join(workDir, "simple.db")
	runExtract(t, tsq, filepath.Join(root, "testdata", "projects", "simple"), dbFile)

	// Query for function names using the existing fixture query.
	queryFile := writeQueryFile(t, workDir, "find_functions.ql",
		"import tsq::functions\n\nfrom Function f\nselect f.getName() as \"name\"\n")

	output := runCLIQuery(t, tsq, dbFile, "csv", queryFile)
	rows := parseCSV(t, output)

	// Expect header row (col0) + at least one data row.
	if len(rows) < 2 {
		t.Fatalf("expected at least 1 result row, got %d total (including header)\noutput:\n%s",
			len(rows), output)
	}

	// CSV format uses positional column names (col0, col1, ...) not QL aliases.
	if len(rows[0]) == 0 || rows[0][0] != "col0" {
		t.Errorf("expected header[0] = \"col0\", got %q", rows[0])
	}

	// Collect function names returned.
	var names []string
	for _, row := range rows[1:] {
		if len(row) > 0 && row[0] != "" {
			names = append(names, row[0])
		}
	}
	if len(names) == 0 {
		t.Fatal("query returned zero function names; expected at least one")
	}

	// The simple fixture defines processData; verify it's present.
	found := false
	for _, n := range names {
		if n == "processData" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected function name \"processData\" in results, got: %v", names)
	}
}

// TestCLI_ExtractAndQuery_SystemRules_LocalFlow verifies that system rules are
// correctly injected by the CLI binary. LocalFlow is a derived relation computed
// entirely by system rules — if MergeSystemRules is not called in compileAndEval,
// the LocalFlow relation will be empty and this test will fail.
//
// This test would have caught the P0 system-rules injection gap described in
// eng-review-apr2026.md.
func TestCLI_ExtractAndQuery_SystemRules_LocalFlow(t *testing.T) {
	tsq := buildTSQBinary(t)
	root := cliRepoRoot(t)
	workDir := t.TempDir()

	// Extract the localflow fixture which has non-trivial assignment chains.
	dbFile := filepath.Join(workDir, "localflow.db")
	runExtract(t, tsq, filepath.Join(root, "testdata", "ts", "v2", "localflow"), dbFile)

	// Query for LocalFlow edges — this relation is populated entirely by system rules.
	// If system rules are not injected, this returns zero rows.
	queryFile := writeQueryFile(t, workDir, "find_localflow.ql",
		"import tsq::dataflow\n\nfrom LocalFlow lf\nselect lf.getSource() as \"src\", lf.getDestination() as \"dst\"\n")

	output := runCLIQuery(t, tsq, dbFile, "csv", queryFile)
	rows := parseCSV(t, output)

	// Must have at least header + 1 data row.
	if len(rows) < 2 {
		t.Fatalf("expected at least 1 LocalFlow result row, got %d total\noutput:\n%s\n"+
			"If this is zero, system rules (MergeSystemRules) are not being injected in the CLI.",
			len(rows), output)
	}

	dataRows := len(rows) - 1 // subtract header
	t.Logf("LocalFlow edges found through CLI: %d", dataRows)

	// Verify the expected header structure (two columns for src and dst).
	if len(rows[0]) != 2 {
		t.Errorf("expected 2 header columns, got %d: %v", len(rows[0]), rows[0])
	}
}

// TestCLI_ExtractAndQuery_CallGraph verifies that the callgraph query returns
// actual results through the CLI. MethodCall is a base-level relation (not
// a system-rules derived relation) but exercises the bridge import loading path.
func TestCLI_ExtractAndQuery_CallGraph(t *testing.T) {
	tsq := buildTSQBinary(t)
	root := cliRepoRoot(t)
	workDir := t.TempDir()

	// Extract a fixture that has method calls (classes has field access and calls).
	dbFile := filepath.Join(workDir, "v2.db")
	runExtract(t, tsq, filepath.Join(root, "testdata", "ts", "v2"), dbFile)

	// Use the existing find_method_calls.ql fixture query.
	queryFile := filepath.Join(root, "testdata", "queries", "v2", "find_method_calls.ql")
	output := runCLIQuery(t, tsq, dbFile, "csv", queryFile)
	rows := parseCSV(t, output)

	// Must have at least header + 1 data row.
	if len(rows) < 2 {
		t.Fatalf("expected at least 1 result row from find_method_calls, got %d total\noutput:\n%s",
			len(rows), output)
	}

	t.Logf("Method call rows found: %d", len(rows)-1)
}
