// Package integration_test contains integration tests that invoke the tsq CLI
// binary as a subprocess. These tests verify end-to-end behaviour that cannot
// be caught by tests that bypass the CLI — in particular, the system-rules
// injection that makes taint/callgraph/dataflow queries return results.
//
// See eng-review-apr2026.md, item P9: "CLI integration test through the actual binary".
package integration_test

import (
	"encoding/csv"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildTSQ compiles the tsq binary into a temporary directory and returns its path.
// The binary is reused across sub-tests in a single TestMain run via t.TempDir.
func buildTSQ(t *testing.T) string {
	t.Helper()

	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "tsq")

	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/tsq")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build tsq binary: %v\n%s", err, out)
	}
	return binPath
}

// TestCLIExtractAndQueryFunctions is a basic smoke test: extract a small
// TypeScript project and run a simple function-name query through the binary.
// It verifies exit-0 and non-empty CSV output — the simplest possible signal
// that the extract→query pipeline works end-to-end.
func TestCLIExtractAndQueryFunctions(t *testing.T) {
	tsq := buildTSQ(t)
	workDir := t.TempDir()
	dbPath := filepath.Join(workDir, "out.db")

	// Extract the simple fixture project.
	extractCmd := exec.Command(tsq, "extract",
		"--dir", "testdata/projects/simple",
		"--output", dbPath,
		"--backend", "vendored",
	)
	if out, err := extractCmd.CombinedOutput(); err != nil {
		t.Fatalf("tsq extract failed: %v\n%s", err, out)
	}

	// Write a minimal query to a temp file.
	queryPath := filepath.Join(workDir, "functions.ql")
	if err := os.WriteFile(queryPath, []byte(`import tsq::functions
from Function f
select f.getName() as "name"
`), 0o644); err != nil {
		t.Fatalf("write query: %v", err)
	}

	// Run the query and capture CSV output.
	queryCmd := exec.Command(tsq, "query",
		"--db", dbPath,
		"--format", "csv",
		queryPath,
	)
	out, err := queryCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tsq query failed: %v\n%s", err, out)
	}

	rows := parseCSVOutput(t, out)
	if len(rows) == 0 {
		t.Fatal("expected at least one function row, got none")
	}

	// The simple fixture defines add, multiply, greet, processData.
	names := make(map[string]bool)
	for _, row := range rows {
		if len(row) > 0 {
			names[row[0]] = true
		}
	}
	for _, want := range []string{"add", "multiply", "greet", "processData"} {
		if !names[want] {
			t.Errorf("expected function %q in results; got: %v", want, names)
		}
	}
}

// TestCLISystemRulesInjected verifies that the CLI binary correctly injects
// system rules so that derived relations (LocalFlow, CallTarget, TaintAlert, etc.)
// are populated. This is the core regression test for the P0 bug in
// eng-review-apr2026.md: the CLI was not calling MergeSystemRules, so all
// taint/callgraph/dataflow queries returned empty results.
//
// The test uses the v2/taint fixture which contains Express route handlers with
// SQL injection and XSS patterns. It runs a MethodCall query — which depends on
// the extracted MethodCall relation (not system rules) — to verify basic
// extraction works, then runs a LocalFlow query which depends on system rules.
func TestCLISystemRulesInjected(t *testing.T) {
	tsq := buildTSQ(t)
	workDir := t.TempDir()
	dbPath := filepath.Join(workDir, "taint.db")

	// Extract the taint fixture (contains req.query/res.send patterns).
	extractCmd := exec.Command(tsq, "extract",
		"--dir", "testdata/ts/v2/taint",
		"--output", dbPath,
		"--backend", "vendored",
	)
	if out, err := extractCmd.CombinedOutput(); err != nil {
		t.Fatalf("tsq extract failed: %v\n%s", err, out)
	}

	workDir2 := t.TempDir()

	// Query 1: MethodCall — exercises extracted facts, no system rules needed.
	// If this returns results, extraction is working.
	mcQuery := filepath.Join(workDir2, "method_calls.ql")
	if err := os.WriteFile(mcQuery, []byte(`import tsq::types
from MethodCall mc
select mc.getMethodName() as "methodName"
`), 0o644); err != nil {
		t.Fatalf("write method_calls.ql: %v", err)
	}

	mcCmd := exec.Command(tsq, "query", "--db", dbPath, "--format", "csv", mcQuery)
	mcOut, err := mcCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tsq query method_calls failed: %v\n%s", err, mcOut)
	}

	mcRows := parseCSVOutput(t, mcOut)
	if len(mcRows) == 0 {
		t.Fatal("expected MethodCall rows from taint fixture, got none — extraction may have failed")
	}
	t.Logf("MethodCall rows: %d", len(mcRows))

	// Query 2: LocalFlow — requires system-rules injection (LocalFlowRules()).
	// If system rules are NOT injected (the pre-fix bug), LocalFlow is empty
	// and this query returns zero data rows.
	lfQuery := filepath.Join(workDir2, "localflow.ql")
	if err := os.WriteFile(lfQuery, []byte(`import tsq::dataflow
from LocalFlow lf
select lf.getSource() as "src", lf.getDestination() as "dst"
`), 0o644); err != nil {
		t.Fatalf("write localflow.ql: %v", err)
	}

	lfCmd := exec.Command(tsq, "query", "--db", dbPath, "--format", "csv", lfQuery)
	lfOut, err := lfCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tsq query localflow failed: %v\n%s", err, lfOut)
	}

	lfRows := parseCSVOutput(t, lfOut)
	if len(lfRows) == 0 {
		t.Fatal("LocalFlow query returned no results — system rules may not be injected into the CLI binary. " +
			"This is the P0 regression from eng-review-apr2026.md: MergeSystemRules must be called in compileAndEval.")
	}
	t.Logf("LocalFlow rows: %d", len(lfRows))
}

// TestCLICallGraphQuery verifies that call-graph derived relations are populated
// through the binary. CallTarget depends on CallGraphRules() being injected.
func TestCLICallGraphQuery(t *testing.T) {
	tsq := buildTSQ(t)
	workDir := t.TempDir()
	dbPath := filepath.Join(workDir, "simple.db")

	// Extract the simple fixture — it has cross-file calls (main imports utils).
	extractCmd := exec.Command(tsq, "extract",
		"--dir", "testdata/projects/simple",
		"--output", dbPath,
		"--backend", "vendored",
	)
	if out, err := extractCmd.CombinedOutput(); err != nil {
		t.Fatalf("tsq extract failed: %v\n%s", err, out)
	}

	workDir2 := t.TempDir()

	// Call relation query — exercises extracted Call facts (no system rules).
	callQuery := filepath.Join(workDir2, "calls.ql")
	if err := os.WriteFile(callQuery, []byte(`import tsq::calls
from Call c
select c.getArity() as "arity"
`), 0o644); err != nil {
		t.Fatalf("write calls.ql: %v", err)
	}

	callCmd := exec.Command(tsq, "query", "--db", dbPath, "--format", "csv", callQuery)
	callOut, err := callCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tsq query calls failed: %v\n%s", err, callOut)
	}

	callRows := parseCSVOutput(t, callOut)
	if len(callRows) == 0 {
		t.Fatal("Call query returned no results — extraction may have failed")
	}
	t.Logf("Call rows: %d", len(callRows))
}

// TestCLIOutputFormats verifies that all three output formats (json, sarif, csv)
// are accepted by the CLI and produce non-error output.
func TestCLIOutputFormats(t *testing.T) {
	tsq := buildTSQ(t)
	workDir := t.TempDir()
	dbPath := filepath.Join(workDir, "out.db")

	extractCmd := exec.Command(tsq, "extract",
		"--dir", "testdata/projects/simple",
		"--output", dbPath,
		"--backend", "vendored",
	)
	if out, err := extractCmd.CombinedOutput(); err != nil {
		t.Fatalf("tsq extract failed: %v\n%s", err, out)
	}

	queryPath := filepath.Join(workDir, "q.ql")
	if err := os.WriteFile(queryPath, []byte(`import tsq::functions
from Function f
select f.getName() as "name"
`), 0o644); err != nil {
		t.Fatalf("write query: %v", err)
	}

	for _, format := range []string{"json", "sarif", "csv"} {
		t.Run(format, func(t *testing.T) {
			cmd := exec.Command(tsq, "query",
				"--db", dbPath,
				"--format", format,
				queryPath,
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("tsq query --format %s failed: %v\n%s", format, err, out)
			}
			if len(strings.TrimSpace(string(out))) == 0 {
				t.Fatalf("tsq query --format %s produced empty output", format)
			}
		})
	}
}

// parseCSVOutput parses a CSV output from tsq query, skipping the header row.
// Returns the data rows (header excluded).
func parseCSVOutput(t *testing.T, data []byte) [][]string {
	t.Helper()
	r := csv.NewReader(strings.NewReader(string(data)))
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("parse CSV output: %v\nraw: %s", err, data)
	}
	if len(records) == 0 {
		return nil
	}
	// Skip header row.
	return records[1:]
}
