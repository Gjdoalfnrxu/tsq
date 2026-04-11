package bridge

import (
	"fmt"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/ast"
	"github.com/Gjdoalfnrxu/tsq/ql/desugar"
	"github.com/Gjdoalfnrxu/tsq/ql/parse"
	"github.com/Gjdoalfnrxu/tsq/ql/resolve"
)

// makeDataFlowImportLoader creates an import loader that resolves
// "DataFlow::PathGraph" and all tsq:: paths to the appropriate bridge file.
func makeDataFlowImportLoader() func(string) (*ast.Module, error) {
	bridgeFiles := LoadBridge()
	pathToFile := map[string]string{
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

// TestCompatDataFlowParses verifies the compat_dataflow.qll file parses without errors.
func TestCompatDataFlowParses(t *testing.T) {
	files := LoadBridge()
	data, ok := files["compat_dataflow.qll"]
	if !ok {
		t.Fatal("compat_dataflow.qll not found in embedded bridge files")
	}
	p := parse.NewParser(string(data), "compat_dataflow.qll")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(mod.Modules) == 0 {
		t.Error("compat_dataflow.qll parsed but contains no module declarations")
	}
}

// TestCompatDataFlowRegistersClasses verifies that after resolve, the DataFlow
// module and its classes (Node, Configuration, PathNode) are registered.
func TestCompatDataFlowRegistersClasses(t *testing.T) {
	files := LoadBridge()
	data, ok := files["compat_dataflow.qll"]
	if !ok {
		t.Fatal("compat_dataflow.qll not found")
	}
	p := parse.NewParser(string(data), "compat_dataflow.qll")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("resolve returned fatal error: %v", err)
	}

	// Verify the DataFlow module is registered.
	if _, ok := rm.Env.Modules["DataFlow"]; !ok {
		t.Error("DataFlow module not registered")
	}

	// Verify qualified class names are registered.
	expectedClasses := []string{
		"DataFlow::Node",
		"DataFlow::Configuration",
		"DataFlow::PathNode",
	}
	for _, name := range expectedClasses {
		if _, ok := rm.Env.Classes[name]; !ok {
			t.Errorf("expected class %q not registered", name)
		}
	}
}

// TestImportDataFlowPathGraphQuery verifies that a query using
// `import DataFlow::PathGraph` with DataFlow::Node parses, resolves,
// and desugars without errors end-to-end.
func TestImportDataFlowPathGraphQuery(t *testing.T) {
	query := `import DataFlow::PathGraph

from DataFlow::Node n
select n
`
	p := parse.NewParser(query, "test_dataflow.ql")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(mod.Imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(mod.Imports))
	}
	if mod.Imports[0].Path != "DataFlow::PathGraph" {
		t.Fatalf("expected import path 'DataFlow::PathGraph', got %q", mod.Imports[0].Path)
	}

	loader := makeDataFlowImportLoader()
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

	// Verify DataFlow::Node class is available.
	if _, ok := rm.Env.Classes["DataFlow::Node"]; !ok {
		t.Error("DataFlow::Node class not available after import DataFlow::PathGraph")
	}

	// Desugar.
	_, dsErrors := desugar.Desugar(rm)
	if len(dsErrors) > 0 {
		for _, e := range dsErrors {
			t.Errorf("desugar error: %v", e)
		}
	}
}

// TestDataFlowModuleHasPredicates verifies that the DataFlow module
// exports hasFlow and hasFlowPath predicates.
func TestDataFlowModuleHasPredicates(t *testing.T) {
	files := LoadBridge()
	data, ok := files["compat_dataflow.qll"]
	if !ok {
		t.Fatal("compat_dataflow.qll not found")
	}
	p := parse.NewParser(string(data), "compat_dataflow.qll")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("resolve returned fatal error: %v", err)
	}

	expectedPredicates := []string{
		"DataFlow::hasFlow",
		"DataFlow::hasFlowPath",
	}
	for _, name := range expectedPredicates {
		if _, ok := rm.Env.Predicates[name]; !ok {
			t.Errorf("expected predicate %q not registered", name)
		}
	}
}

// TestDataFlowNodeMembers verifies DataFlow::Node has the expected member predicates.
func TestDataFlowNodeMembers(t *testing.T) {
	files := LoadBridge()
	data := files["compat_dataflow.qll"]
	p := parse.NewParser(string(data), "compat_dataflow.qll")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	nodeClass, ok := rm.Env.Classes["DataFlow::Node"]
	if !ok {
		t.Fatal("DataFlow::Node class not found")
	}

	expectedMembers := map[string]bool{
		"toString":    false,
		"getLocation": false,
		"asExpr":      false,
		"getName":     false,
	}

	for _, m := range nodeClass.Members {
		if _, want := expectedMembers[m.Name]; want {
			expectedMembers[m.Name] = true
		}
	}

	for name, found := range expectedMembers {
		if !found {
			t.Errorf("DataFlow::Node missing expected member %q", name)
		}
	}
}
