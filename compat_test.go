package integration_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Gjdoalfnrxu/tsq/bridge"
	"github.com/Gjdoalfnrxu/tsq/extract/rules"
	"github.com/Gjdoalfnrxu/tsq/ql/ast"
	"github.com/Gjdoalfnrxu/tsq/ql/desugar"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/parse"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
	"github.com/Gjdoalfnrxu/tsq/ql/resolve"

	"github.com/Gjdoalfnrxu/tsq/extract/db"
)

// makeCompatImportLoader creates an import loader that resolves CodeQL-compat
// import paths (javascript, DataFlow::PathGraph, TaintTracking::PathGraph,
// semmle.* security libraries) as well as all tsq:: paths.
func makeCompatImportLoader(bridgeFiles map[string][]byte) func(string) (*ast.Module, error) {
	pathToFile := map[string]string{
		// CodeQL-compat paths
		"javascript":                                            "compat_javascript.qll",
		"DataFlow::PathGraph":                                   "compat_dataflow.qll",
		"TaintTracking::PathGraph":                              "compat_tainttracking.qll",
		"semmle.javascript.security.dataflow.XssQuery":          "compat_security_xss.qll",
		"semmle.javascript.security.dataflow.SqlInjectionQuery": "compat_security_sqli.qll",

		// tsq:: internal paths
		"tsq::base":        "tsq_base.qll",
		"tsq::functions":   "tsq_functions.qll",
		"tsq::calls":       "tsq_calls.qll",
		"tsq::variables":   "tsq_variables.qll",
		"tsq::expressions": "tsq_expressions.qll",
		"tsq::jsx":         "tsq_jsx.qll",
		"tsq::imports":     "tsq_imports.qll",
		"tsq::errors":      "tsq_errors.qll",
		"tsq::types":       "tsq_types.qll",
		"tsq::symbols":     "tsq_symbols.qll",
		"tsq::callgraph":   "tsq_callgraph.qll",
		"tsq::dataflow":    "tsq_dataflow.qll",
		"tsq::summaries":   "tsq_summaries.qll",
		"tsq::composition": "tsq_composition.qll",
		"tsq::taint":       "tsq_taint.qll",
		"tsq::express":     "tsq_express.qll",
		"tsq::react":       "tsq_react.qll",
		"tsq::node":        "tsq_node.qll",
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

// runCompatQuery compiles and evaluates a QL query using the compat import
// loader, which supports CodeQL-style imports (import javascript, etc.).
func runCompatQuery(t *testing.T, queryFile string, factDB *db.DB) *eval.ResultSet {
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

	// Resolve with compat import loader
	bridgeFiles := bridge.LoadBridge()
	importLoader := makeCompatImportLoader(bridgeFiles)
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

	// Merge system rules (taint propagation, call graph, local flow, etc.)
	// so derived relations like TaintAlert are computed during evaluation.
	prog = rules.MergeSystemRules(prog, rules.AllSystemRules())

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

// compatTestCase defines a compat (project, query) pair for golden testing.
type compatTestCase struct {
	name       string
	projectDir string
	queryFile  string
	goldenFile string
	skip       string // non-empty to skip with this reason (placeholder for future plans)
}

func compatTestCases() []compatTestCase {
	return []compatTestCase{
		{
			name:       "ast_query",
			projectDir: "testdata/compat/projects/basic",
			queryFile:  "testdata/compat/ast_query.ql",
			goldenFile: "testdata/compat/expected/ast_query.csv",
		},
		{
			name:       "find_xss",
			projectDir: "testdata/compat/projects/basic",
			queryFile:  "testdata/compat/find_xss.ql",
			goldenFile: "testdata/compat/expected/find_xss.csv",
		},
		{
			name:       "find_sqli",
			projectDir: "testdata/compat/projects/basic",
			queryFile:  "testdata/compat/find_sqli.ql",
			goldenFile: "testdata/compat/expected/find_sqli.csv",
		},
		{
			name:       "custom_config",
			projectDir: "testdata/compat/projects/basic",
			queryFile:  "testdata/compat/custom_config.ql",
			goldenFile: "testdata/compat/expected/custom_config.csv",
			// A2: Configuration override dispatch works; hasFlow disjunction grounding fixed in A3
		},
		{
			name:       "dataflow_predicates",
			projectDir: "testdata/compat/projects/basic",
			queryFile:  "testdata/compat/dataflow_predicates.ql",
			goldenFile: "testdata/compat/expected/dataflow_predicates.csv",
		},
	}
}

// TestCompat runs all compat end-to-end test cases.
func TestCompat(t *testing.T) {
	// Cache extracted DBs to avoid re-extracting for the same project.
	dbCache := make(map[string]*db.DB)

	for _, tc := range compatTestCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.skip != "" {
				t.Skip(tc.skip)
			}

			factDB, ok := dbCache[tc.projectDir]
			if !ok {
				raw := extractProject(t, tc.projectDir)
				factDB = serializeDB(t, raw)
				dbCache[tc.projectDir] = factDB
			}

			rs := runCompatQuery(t, tc.queryFile, factDB)
			got := resultToCSV(rs)

			// Guard against empty results: an empty golden is almost
			// certainly a bug — the fixture files are designed to produce
			// at least one match.
			if len(rs.Rows) == 0 && !*updateGolden {
				t.Fatal("query returned zero rows — expected at least one result from the fixture data")
			}
			if len(rs.Rows) == 0 && *updateGolden {
				t.Fatal("query returned zero rows — refusing to write an empty golden file; check the query and fixture data")
			}

			compareGolden(t, tc.goldenFile, got)
		})
	}
}
