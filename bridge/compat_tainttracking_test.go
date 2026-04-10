package bridge

import (
	"fmt"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/ast"
	"github.com/Gjdoalfnrxu/tsq/ql/desugar"
	"github.com/Gjdoalfnrxu/tsq/ql/parse"
	"github.com/Gjdoalfnrxu/tsq/ql/resolve"
)

// makeTaintTrackingImportLoader creates an import loader that resolves
// "TaintTracking" and all tsq:: paths to the appropriate bridge file.
func makeTaintTrackingImportLoader() func(string) (*ast.Module, error) {
	bridgeFiles := LoadBridge()
	pathToFile := map[string]string{
		"TaintTracking":       "compat_tainttracking.qll",
		"DataFlow::PathGraph": "compat_dataflow.qll",
		"javascript":          "compat_javascript.qll",
		"tsq::base":           "tsq_base.qll",
		"tsq::functions":      "tsq_functions.qll",
		"tsq::calls":          "tsq_calls.qll",
		"tsq::variables":      "tsq_variables.qll",
		"tsq::expressions":    "tsq_expressions.qll",
		"tsq::types":          "tsq_types.qll",
		"tsq::imports":        "tsq_imports.qll",
		"tsq::symbols":        "tsq_symbols.qll",
		"tsq::dataflow":       "tsq_dataflow.qll",
		"tsq::taint":          "tsq_taint.qll",
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

// TestCompatTaintTrackingParses verifies the compat_tainttracking.qll file parses without errors.
func TestCompatTaintTrackingParses(t *testing.T) {
	files := LoadBridge()
	data, ok := files["compat_tainttracking.qll"]
	if !ok {
		t.Fatal("compat_tainttracking.qll not found in embedded bridge files")
	}
	p := parse.NewParser(string(data), "compat_tainttracking.qll")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(mod.Modules) == 0 {
		t.Error("compat_tainttracking.qll parsed but contains no module declarations")
	}
}

// TestCompatTaintTrackingRegistersClasses verifies that after resolve, the TaintTracking
// module and its Configuration class are registered.
func TestCompatTaintTrackingRegistersClasses(t *testing.T) {
	files := LoadBridge()
	data, ok := files["compat_tainttracking.qll"]
	if !ok {
		t.Fatal("compat_tainttracking.qll not found")
	}
	p := parse.NewParser(string(data), "compat_tainttracking.qll")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("resolve returned fatal error: %v", err)
	}

	// Verify the TaintTracking module is registered.
	if _, ok := rm.Env.Modules["TaintTracking"]; !ok {
		t.Error("TaintTracking module not registered")
	}

	// Verify qualified class name is registered.
	if _, ok := rm.Env.Classes["TaintTracking::Configuration"]; !ok {
		t.Error("expected class TaintTracking::Configuration not registered")
	}
}

// TestImportTaintTrackingQuery verifies that a query using
// `import TaintTracking` with TaintTracking::Configuration parses, resolves,
// and desugars without errors end-to-end.
func TestImportTaintTrackingQuery(t *testing.T) {
	query := `import TaintTracking

from TaintTracking::Configuration cfg
select cfg
`
	p := parse.NewParser(query, "test_tainttracking.ql")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(mod.Imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(mod.Imports))
	}
	if mod.Imports[0].Path != "TaintTracking" {
		t.Fatalf("expected import path 'TaintTracking', got %q", mod.Imports[0].Path)
	}

	loader := makeTaintTrackingImportLoader()
	rm, err := resolve.Resolve(mod, loader)
	if err != nil {
		t.Fatalf("resolve error: %v", err)
	}
	if len(rm.Errors) > 0 {
		for _, e := range rm.Errors {
			t.Errorf("resolve error: %v", e)
		}
		t.FailNow()
	}

	// Verify TaintTracking::Configuration class is available.
	if _, ok := rm.Env.Classes["TaintTracking::Configuration"]; !ok {
		t.Error("TaintTracking::Configuration class not available after import TaintTracking")
	}

	// Desugar.
	_, dsErrors := desugar.Desugar(rm)
	if len(dsErrors) > 0 {
		for _, e := range dsErrors {
			t.Errorf("desugar error: %v", e)
		}
	}
}

// TestTaintTrackingModuleHasPredicates verifies that the TaintTracking module
// exports hasFlow and hasFlowPath predicates.
func TestTaintTrackingModuleHasPredicates(t *testing.T) {
	files := LoadBridge()
	data, ok := files["compat_tainttracking.qll"]
	if !ok {
		t.Fatal("compat_tainttracking.qll not found")
	}
	p := parse.NewParser(string(data), "compat_tainttracking.qll")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("resolve returned fatal error: %v", err)
	}

	expectedPredicates := []string{
		"TaintTracking::hasFlow",
		"TaintTracking::hasFlowPath",
	}
	for _, name := range expectedPredicates {
		if _, ok := rm.Env.Predicates[name]; !ok {
			t.Errorf("expected predicate %q not registered", name)
		}
	}
}
