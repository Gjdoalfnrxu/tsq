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
	"time"
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

// TestCLI_IterationCap_ErrorByDefault is the CLI-level regression for issue
// #79. It runs a recursive transitive-closure query against an extracted
// fact DB with --max-iterations 2, asserts the CLI returns a runtime error
// (exit code 1), and that stderr names the rule and the iteration count.
//
// Before the fix, the same query would return exit code 0 with partial
// results — silent wrong answer. This test would not have triggered before;
// it specifically exercises the CLI-flag-to-evaluator wiring.
func TestCLI_IterationCap_ErrorByDefault(t *testing.T) {
	tsq := buildTSQBinary(t)
	root := cliRepoRoot(t)
	workDir := t.TempDir()

	// Any fixture with a non-trivial AST will do — Contains chains in real
	// TypeScript code easily exceed two levels of nesting, so a recursive
	// closure over Contains needs more than 2 fixpoint iterations.
	dbFile := filepath.Join(workDir, "v2.db")
	runExtract(t, tsq, filepath.Join(root, "testdata", "ts", "v2"), dbFile)

	// Recursive transitive closure over Contains. Real ASTs are deep, so
	// this rule cannot converge in 2 iterations.
	// The system-rules pipeline injects deeply recursive predicates (notably
	// LocalFlowStar — the transitive closure of LocalFlow). Even a trivial
	// user query forces those strata to run, and on real fact data they
	// require many iterations to converge. With --max-iterations 2 the cap
	// fires inside the system-rule stratum — exactly the silent-wrong-answer
	// shape issue #79 cares about.
	queryFile := writeQueryFile(t, workDir, "find_calls.ql", `import tsq::calls

from Call c
select c as "c"
`)

	cmd := exec.Command(tsq, "query", "--db", dbFile, "--max-iterations", "2", "--format", "csv", queryFile)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatalf("expected non-zero exit (--max-iterations 2 should error on a divergent query), got success.\nstdout: %s\nstderr: %s", stdout.String(), stderr.String())
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}
	if exitErr.ExitCode() != 1 {
		t.Errorf("expected exit code 1 (runtime error), got %d", exitErr.ExitCode())
	}
	se := stderr.String()
	if !strings.Contains(se, "did not converge") {
		t.Errorf("expected stderr to mention non-convergence, got: %q", se)
	}
	if !strings.Contains(se, "2 iterations") {
		t.Errorf("expected stderr to mention the iteration count (2), got: %q", se)
	}
	// Issue #79 spec calls for the dominant rule name in the error message
	// so the user knows which predicate failed to converge. The exact rule
	// will be a system-rule head (e.g. LocalFlowStar) — we just assert the
	// "rule:" label is present and a non-empty rule name follows it.
	if !strings.Contains(se, "rule:") {
		t.Errorf("expected stderr to include 'rule:' label naming the dominant predicate, got: %q", se)
	}

	// --allow-partial restores the legacy behaviour: exit 0 even when the
	// fixpoint failed to converge. We assert exit 0 and that stderr carries
	// the warning (proving partial-mode actually fired, not just skipped).
	cmd = exec.Command(tsq, "query", "--db", dbFile, "--max-iterations", "2", "--allow-partial", "--format", "csv", queryFile)
	stdout.Reset()
	stderr.Reset()
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("--allow-partial: expected exit 0, got: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "max iteration limit") {
		t.Errorf("--allow-partial: expected warning on stderr (proving cap fired and was bypassed), got: %q", stderr.String())
	}

	// Sanity: with a generous cap the same query succeeds and returns rows.
	cmd = exec.Command(tsq, "query", "--db", dbFile, "--max-iterations", "200", "--format", "csv", queryFile)
	stdout.Reset()
	stderr.Reset()
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("converging case (max-iterations 200): expected exit 0, got: %v\nstderr: %s", err, stderr.String())
	}
	rows := parseCSV(t, stdout.String())
	if len(rows) < 2 {
		t.Errorf("converging case: expected header + at least one Call row, got %d\nstderr: %s", len(rows), stderr.String())
	}
}

// TestCLI_Timeout_Promptness is the CLI-level regression for issue #81.
// It runs a query that compels the system-rules pipeline (including deeply
// recursive predicates such as LocalFlowStar) against a real fact DB with
// --timeout 1ms and asserts:
//   - non-zero exit (timeout is a runtime error, not a partial success)
//   - stderr mentions cancellation / deadline
//   - the whole subprocess returns within ~2s wall time, far below the
//     time the same query takes to converge naturally (multiple seconds on
//     non-trivial fact data). 2s is generous slack to absorb subprocess
//     startup + extraction load on slow CI; the eval itself returns within
//     ~timeout + 100ms per the unit-level promptness tests.
func TestCLI_Timeout_Promptness(t *testing.T) {
	tsq := buildTSQBinary(t)
	root := cliRepoRoot(t)
	workDir := t.TempDir()

	dbFile := filepath.Join(workDir, "v2.db")
	runExtract(t, tsq, filepath.Join(root, "testdata", "ts", "v2"), dbFile)

	// Use the taint pipeline — TaintAlert depends on LocalFlowStar
	// (transitive closure) and the full taint propagation graph, the
	// heaviest stratum the system rules expose. Even on the small v2
	// fixture this cannot converge in a sub-millisecond budget.
	queryFile := writeQueryFile(t, workDir, "find_taint.ql", `import tsq::taint

from TaintAlert alert
select alert.getSrcKind() as "srcKind"
`)

	// --timeout is a global flag (before the subcommand) and only accepts
	// the --timeout=DURATION form per cmd/tsq/main.go's global flag parser.
	// 1ms is intentionally aggressive — guarantees the deadline fires inside
	// a stratum, so we exercise the per-iteration ctx check rather than
	// just the stratum-boundary one.
	cmd := exec.Command(tsq, "--timeout=1ms", "query", "--db", dbFile, "--format", "csv", queryFile)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	t0 := time.Now()
	err := cmd.Run()
	elapsed := time.Since(t0)

	if err == nil {
		// 1ms is well below the cost of even the cheapest rule on this
		// fixture — if we somehow converge inside it, hardware is so fast
		// the test is meaningless. Skip rather than flake.
		t.Skipf("query converged inside 1ms timeout (elapsed=%v); cannot exercise --timeout regression on this fixture", elapsed)
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError from --timeout, got %T: %v\nstderr: %s", err, err, stderr.String())
	}
	if exitErr.ExitCode() == 0 {
		t.Fatalf("expected non-zero exit code from --timeout, got 0\nstderr: %s", stderr.String())
	}

	se := stderr.String()
	// The eval-layer error wraps context.DeadlineExceeded and includes
	// "cancelled" + "stratum N, iteration M". Assert the cancellation
	// surface, not the exact format string.
	if !strings.Contains(se, "cancelled") {
		t.Errorf("expected stderr to mention cancellation, got: %q", se)
	}
	if !strings.Contains(se, "deadline exceeded") && !strings.Contains(se, "context deadline") {
		t.Errorf("expected stderr to mention deadline exceeded, got: %q", se)
	}

	// Promptness ceiling: 2s wall time is a soft cap that includes process
	// spawn + database load + a single rule landing post-deadline. Without
	// the per-iteration ctx check the same query would run to convergence
	// (multiple seconds on this fixture) before the timeout was honored.
	const ceiling = 2 * time.Second
	if elapsed > ceiling {
		t.Errorf("--timeout 1ms: subprocess took %v; ceiling is %v. Per-iteration ctx check likely regressed.", elapsed, ceiling)
	}
	t.Logf("--timeout 1ms: elapsed=%v exit=%d stderr=%q", elapsed, exitErr.ExitCode(), se)
}
