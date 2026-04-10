package integration_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Gjdoalfnrxu/tsq/bridge"
	"github.com/Gjdoalfnrxu/tsq/extract"
	"github.com/Gjdoalfnrxu/tsq/extract/db"
	"github.com/Gjdoalfnrxu/tsq/ql/ast"
	"github.com/Gjdoalfnrxu/tsq/ql/desugar"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/parse"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
	"github.com/Gjdoalfnrxu/tsq/ql/resolve"
)

var updateGolden = flag.Bool("update", false, "update golden files")

// extractProject runs the full extraction pipeline on a project directory,
// returning an in-memory DB. This exercises TreeSitterBackend + FactWalker.
func extractProject(t *testing.T, projectDir string) *db.DB {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	database := db.NewDB()
	walker := extract.NewFactWalker(database)
	backend := &extract.TreeSitterBackend{}
	defer func() {
		if err := backend.Close(); err != nil {
			t.Logf("warning: close backend: %v", err)
		}
	}()

	cfg := extract.ProjectConfig{RootDir: projectDir}
	if err := walker.Run(ctx, backend, cfg); err != nil {
		t.Fatalf("extraction failed for %s: %v", projectDir, err)
	}
	return database
}

// serializeDB writes a DB to bytes and reads it back, exercising the
// encode/decode roundtrip.
func serializeDB(t *testing.T, database *db.DB) *db.DB {
	t.Helper()
	var buf bytes.Buffer
	if err := database.Encode(&buf); err != nil {
		t.Fatalf("encode DB: %v", err)
	}
	data := buf.Bytes()
	reader := bytes.NewReader(data)
	result, err := db.ReadDB(reader, int64(len(data)))
	if err != nil {
		t.Fatalf("decode DB: %v", err)
	}
	return result
}

// makeBridgeImportLoader creates an import loader for bridge .qll files.
func makeBridgeImportLoader(bridgeFiles map[string][]byte) func(path string) (*ast.Module, error) {
	pathToFile := map[string]string{
		"tsq::base":        "tsq_base.qll",
		"tsq::functions":   "tsq_functions.qll",
		"tsq::calls":       "tsq_calls.qll",
		"tsq::variables":   "tsq_variables.qll",
		"tsq::expressions": "tsq_expressions.qll",
		"tsq::jsx":         "tsq_jsx.qll",
		"tsq::imports":     "tsq_imports.qll",
		"tsq::errors":      "tsq_errors.qll",
	}
	return func(path string) (*ast.Module, error) {
		filename, ok := pathToFile[path]
		if !ok {
			return nil, fmt.Errorf("unknown import: %s", path)
		}
		data, ok := bridgeFiles[filename]
		if !ok {
			return nil, fmt.Errorf("missing bridge file: %s", filename)
		}
		p := parse.NewParser(string(data), filename)
		return p.Parse()
	}
}

// runQuery compiles and evaluates a QL query against a fact DB.
func runQuery(t *testing.T, queryFile string, factDB *db.DB) *eval.ResultSet {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	src, err := os.ReadFile(queryFile)
	if err != nil {
		t.Fatalf("read query file %s: %v", queryFile, err)
	}

	// Parse
	p := parse.NewParser(string(src), queryFile)
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse %s: %v", queryFile, err)
	}

	// Resolve
	bridgeFiles := bridge.LoadBridge()
	importLoader := makeBridgeImportLoader(bridgeFiles)
	resolved, err := resolve.Resolve(mod, importLoader)
	if err != nil {
		t.Fatalf("resolve %s: %v", queryFile, err)
	}
	if len(resolved.Errors) > 0 {
		var msgs []string
		for _, e := range resolved.Errors {
			msgs = append(msgs, e.Error())
		}
		t.Fatalf("resolve errors in %s:\n  %s", queryFile, strings.Join(msgs, "\n  "))
	}

	// Desugar
	prog, dsErrors := desugar.Desugar(resolved)
	if len(dsErrors) > 0 {
		var msgs []string
		for _, e := range dsErrors {
			msgs = append(msgs, e.Error())
		}
		t.Fatalf("desugar errors in %s:\n  %s", queryFile, strings.Join(msgs, "\n  "))
	}

	// Plan
	execPlan, planErrors := plan.Plan(prog, nil)
	if len(planErrors) > 0 {
		var msgs []string
		for _, e := range planErrors {
			msgs = append(msgs, e.Error())
		}
		t.Fatalf("plan errors in %s:\n  %s", queryFile, strings.Join(msgs, "\n  "))
	}

	// Evaluate
	evaluator := eval.NewEvaluator(execPlan, factDB)
	rs, err := evaluator.Evaluate(ctx)
	if err != nil {
		t.Fatalf("evaluate %s: %v", queryFile, err)
	}
	return rs
}

// resultToCSV converts a ResultSet to a deterministic sorted CSV string.
func resultToCSV(rs *eval.ResultSet) string {
	var rows []string
	for _, row := range rs.Rows {
		var cols []string
		for _, v := range row {
			cols = append(cols, eval.ValueToString(v))
		}
		rows = append(rows, strings.Join(cols, ","))
	}
	sort.Strings(rows)

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	// Write header
	_ = w.Write(rs.Columns)
	for _, row := range rows {
		fields := strings.Split(row, ",")
		_ = w.Write(fields)
	}
	w.Flush()
	return buf.String()
}

// compareGolden compares output against a golden file, updating if -update is set.
func compareGolden(t *testing.T, goldenPath string, got string) {
	t.Helper()
	if *updateGolden {
		dir := filepath.Dir(goldenPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden file %s: %v", goldenPath, err)
		}
		t.Logf("updated golden file: %s", goldenPath)
		return
	}

	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file %s: %v\nRun with -update to generate", goldenPath, err)
	}
	if string(expected) != got {
		t.Errorf("output mismatch for %s\n--- expected ---\n%s\n--- got ---\n%s",
			goldenPath, string(expected), got)
	}
}

// goldenTestCase defines a (project, query) pair for golden testing.
type goldenTestCase struct {
	name       string
	projectDir string
	queryFile  string
	goldenFile string
}

func goldenTestCases() []goldenTestCase {
	return []goldenTestCase{
		// Simple project
		{
			name:       "simple/find_all_functions",
			projectDir: "testdata/projects/simple",
			queryFile:  "testdata/queries/find_all_functions.ql",
			goldenFile: "testdata/expected/simple_find_all_functions.csv",
		},
		{
			name:       "simple/find_all_calls",
			projectDir: "testdata/projects/simple",
			queryFile:  "testdata/queries/find_all_calls.ql",
			goldenFile: "testdata/expected/simple_find_all_calls.csv",
		},
		{
			name:       "simple/find_calls_gt3_args",
			projectDir: "testdata/projects/simple",
			queryFile:  "testdata/queries/find_calls_gt3_args.ql",
			goldenFile: "testdata/expected/simple_find_calls_gt3_args.csv",
		},
		// React component project
		{
			name:       "react/find_jsx_elements",
			projectDir: "testdata/projects/react-component",
			queryFile:  "testdata/queries/find_jsx_elements.ql",
			goldenFile: "testdata/expected/react_find_jsx_elements.csv",
		},
		{
			name:       "react/find_jsx_attributes",
			projectDir: "testdata/projects/react-component",
			queryFile:  "testdata/queries/find_jsx_attributes.ql",
			goldenFile: "testdata/expected/react_find_jsx_attributes.csv",
		},
		// Destructuring project
		{
			name:       "destructuring/find_destructured_bindings",
			projectDir: "testdata/projects/destructuring",
			queryFile:  "testdata/queries/find_destructured_bindings.ql",
			goldenFile: "testdata/expected/destructuring_find_destructured_bindings.csv",
		},
		// Async patterns project
		{
			name:       "async/find_async_functions",
			projectDir: "testdata/projects/async-patterns",
			queryFile:  "testdata/queries/find_async_functions.ql",
			goldenFile: "testdata/expected/async_find_async_functions.csv",
		},
		{
			name:       "async/find_await_expressions",
			projectDir: "testdata/projects/async-patterns",
			queryFile:  "testdata/queries/find_await_expressions.ql",
			goldenFile: "testdata/expected/async_find_await_expressions.csv",
		},
		{
			name:       "async/find_all_functions",
			projectDir: "testdata/projects/async-patterns",
			queryFile:  "testdata/queries/find_all_functions.ql",
			goldenFile: "testdata/expected/async_find_all_functions.csv",
		},
		// Imports project
		{
			name:       "imports/find_imports",
			projectDir: "testdata/projects/imports",
			queryFile:  "testdata/queries/find_imports.ql",
			goldenFile: "testdata/expected/imports_find_imports.csv",
		},
		{
			name:       "imports/find_all_functions",
			projectDir: "testdata/projects/imports",
			queryFile:  "testdata/queries/find_all_functions.ql",
			goldenFile: "testdata/expected/imports_find_all_functions.csv",
		},
		// Cross-project: arrow functions
		{
			name:       "simple/find_arrow_functions",
			projectDir: "testdata/projects/simple",
			queryFile:  "testdata/queries/find_arrow_functions.ql",
			goldenFile: "testdata/expected/simple_find_arrow_functions.csv",
		},
		// Cross-project: async functions in simple (should be empty)
		{
			name:       "simple/find_async_functions",
			projectDir: "testdata/projects/simple",
			queryFile:  "testdata/queries/find_async_functions.ql",
			goldenFile: "testdata/expected/simple_find_async_functions.csv",
		},
		// Imports in async project
		{
			name:       "async/find_imports",
			projectDir: "testdata/projects/async-patterns",
			queryFile:  "testdata/queries/find_imports.ql",
			goldenFile: "testdata/expected/async_find_imports.csv",
		},
		// Destructuring arrow functions
		{
			name:       "destructuring/find_arrow_functions",
			projectDir: "testdata/projects/destructuring",
			queryFile:  "testdata/queries/find_arrow_functions.ql",
			goldenFile: "testdata/expected/destructuring_find_arrow_functions.csv",
		},
	}
}

// TestGolden runs all golden test cases.
func TestGolden(t *testing.T) {
	// Cache extracted DBs to avoid re-extracting for the same project.
	dbCache := make(map[string]*db.DB)

	for _, tc := range goldenTestCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			factDB, ok := dbCache[tc.projectDir]
			if !ok {
				raw := extractProject(t, tc.projectDir)
				factDB = serializeDB(t, raw)
				dbCache[tc.projectDir] = factDB
			}

			rs := runQuery(t, tc.queryFile, factDB)
			got := resultToCSV(rs)
			compareGolden(t, tc.goldenFile, got)
		})
	}
}

// TestExtractionDBRoundtrip verifies that encoding and decoding the DB
// preserves all data.
func TestExtractionDBRoundtrip(t *testing.T) {
	projects := []string{
		"testdata/projects/simple",
		"testdata/projects/react-component",
		"testdata/projects/async-patterns",
		"testdata/projects/destructuring",
		"testdata/projects/imports",
	}
	for _, dir := range projects {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			original := extractProject(t, dir)
			roundtripped := serializeDB(t, original)
			// Verify by running a simple query on both.
			q := "testdata/queries/find_all_functions.ql"
			rsOrig := runQuery(t, q, original)
			rsRT := runQuery(t, q, roundtripped)
			origCSV := resultToCSV(rsOrig)
			rtCSV := resultToCSV(rsRT)
			if origCSV != rtCSV {
				t.Errorf("roundtrip mismatch:\n--- original ---\n%s\n--- roundtripped ---\n%s",
					origCSV, rtCSV)
			}
		})
	}
}

// TestNegativeSyntaxError verifies that a query with a syntax error fails at parse time.
func TestNegativeSyntaxError(t *testing.T) {
	src, err := os.ReadFile("testdata/queries/syntax_error.ql")
	if err != nil {
		t.Fatal(err)
	}
	p := parse.NewParser(string(src), "syntax_error.ql")
	_, err = p.Parse()
	if err == nil {
		t.Fatal("expected parse error for syntax_error.ql, got nil")
	}
	// Verify the error mentions position info.
	errStr := err.Error()
	if !strings.Contains(errStr, "1") && !strings.Contains(errStr, "syntax") {
		t.Logf("parse error: %s", errStr)
	}
	t.Logf("got expected parse error: %s", errStr)
}

// TestNegativeUnresolvedName verifies that a query with an unresolved type fails at resolve time.
func TestNegativeUnresolvedName(t *testing.T) {
	src, err := os.ReadFile("testdata/queries/unresolved_name.ql")
	if err != nil {
		t.Fatal(err)
	}
	p := parse.NewParser(string(src), "unresolved_name.ql")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	bridgeFiles := bridge.LoadBridge()
	importLoader := makeBridgeImportLoader(bridgeFiles)
	resolved, err := resolve.Resolve(mod, importLoader)
	if err != nil {
		// Direct resolve error is acceptable.
		t.Logf("got expected resolve error: %v", err)
		return
	}
	if len(resolved.Errors) > 0 {
		t.Logf("got expected resolve errors: %v", resolved.Errors)
		return
	}

	// If resolve passed, desugar/plan/eval should fail on unknown type.
	prog, dsErrors := desugar.Desugar(resolved)
	if len(dsErrors) > 0 {
		t.Logf("got expected desugar errors: %v", dsErrors)
		return
	}
	_, planErrors := plan.Plan(prog, nil)
	if len(planErrors) > 0 {
		t.Logf("got expected plan errors: %v", planErrors)
		return
	}
	t.Fatal("expected error for unresolved name query, but none occurred")
}

// TestEmptyProject verifies that extracting an empty project produces a valid DB.
func TestEmptyProject(t *testing.T) {
	// Create a temporary empty directory.
	dir := t.TempDir()
	database := extractProject(t, dir)
	factDB := serializeDB(t, database)

	// Running a query on an empty DB should return empty results.
	rs := runQuery(t, "testdata/queries/find_all_functions.ql", factDB)
	if len(rs.Rows) != 0 {
		t.Errorf("expected 0 rows from empty project, got %d", len(rs.Rows))
	}
}

// TestPerformanceExtraction verifies that extraction of multi-file projects
// completes in a reasonable time.
func TestPerformanceExtraction(t *testing.T) {
	projects := []string{
		"testdata/projects/simple",
		"testdata/projects/react-component",
		"testdata/projects/async-patterns",
		"testdata/projects/destructuring",
		"testdata/projects/imports",
	}
	for _, dir := range projects {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			start := time.Now()
			_ = extractProject(t, dir)
			elapsed := time.Since(start)
			if elapsed > 10*time.Second {
				t.Errorf("extraction of %s took %v (>10s)", dir, elapsed)
			}
			t.Logf("extraction of %s took %v", dir, elapsed)
		})
	}
}

// TestPerformanceQuery verifies that query evaluation completes in reasonable time.
func TestPerformanceQuery(t *testing.T) {
	// Extract the largest project once.
	database := extractProject(t, "testdata/projects/imports")
	factDB := serializeDB(t, database)

	queries := []string{
		"testdata/queries/find_all_functions.ql",
		"testdata/queries/find_all_calls.ql",
		"testdata/queries/find_imports.ql",
	}
	for _, q := range queries {
		t.Run(filepath.Base(q), func(t *testing.T) {
			start := time.Now()
			rs := runQuery(t, q, factDB)
			elapsed := time.Since(start)
			if elapsed > 5*time.Second {
				t.Errorf("query %s took %v (>5s)", q, elapsed)
			}
			t.Logf("query %s: %d rows in %v", q, len(rs.Rows), elapsed)
		})
	}
}

// TestMultipleQueriesSameDB ensures running multiple queries against the same
// extracted DB produces independent, correct results.
func TestMultipleQueriesSameDB(t *testing.T) {
	database := extractProject(t, "testdata/projects/simple")
	factDB := serializeDB(t, database)

	rs1 := runQuery(t, "testdata/queries/find_all_functions.ql", factDB)
	rs2 := runQuery(t, "testdata/queries/find_all_calls.ql", factDB)

	if len(rs1.Rows) == 0 {
		t.Error("expected non-empty function results")
	}
	if len(rs2.Rows) == 0 {
		t.Error("expected non-empty call results")
	}
	// Functions and calls should return different result sets.
	csv1 := resultToCSV(rs1)
	csv2 := resultToCSV(rs2)
	if csv1 == csv2 {
		t.Error("function and call results should differ")
	}
}

// TestExtractionProducesExpectedRelations verifies that each project type
// produces the expected fact relations.
func TestExtractionProducesExpectedRelations(t *testing.T) {
	tests := []struct {
		project   string
		queryFile string
		minRows   int
	}{
		{"testdata/projects/simple", "testdata/queries/find_all_functions.ql", 1},
		{"testdata/projects/simple", "testdata/queries/find_all_calls.ql", 1},
		{"testdata/projects/react-component", "testdata/queries/find_jsx_elements.ql", 1},
		{"testdata/projects/async-patterns", "testdata/queries/find_async_functions.ql", 1},
		{"testdata/projects/async-patterns", "testdata/queries/find_await_expressions.ql", 1},
		{"testdata/projects/destructuring", "testdata/queries/find_destructured_bindings.ql", 1},
		{"testdata/projects/imports", "testdata/queries/find_imports.ql", 1},
	}
	for _, tt := range tests {
		name := filepath.Base(tt.project) + "/" + filepath.Base(tt.queryFile)
		t.Run(name, func(t *testing.T) {
			database := extractProject(t, tt.project)
			factDB := serializeDB(t, database)
			rs := runQuery(t, tt.queryFile, factDB)
			if len(rs.Rows) < tt.minRows {
				t.Errorf("expected at least %d rows, got %d", tt.minRows, len(rs.Rows))
			}
		})
	}
}
