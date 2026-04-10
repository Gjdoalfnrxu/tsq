package bridge

import (
	"fmt"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/ast"
	"github.com/Gjdoalfnrxu/tsq/ql/desugar"
	"github.com/Gjdoalfnrxu/tsq/ql/parse"
	"github.com/Gjdoalfnrxu/tsq/ql/resolve"
)

// makeCompatImportLoader creates an import loader that resolves "javascript"
// (and all tsq:: paths) to the appropriate bridge file.
func makeCompatImportLoader() func(string) (*ast.Module, error) {
	bridgeFiles := LoadBridge()
	pathToFile := map[string]string{
		"javascript":       "compat_javascript.qll",
		"tsq::base":        "tsq_base.qll",
		"tsq::functions":   "tsq_functions.qll",
		"tsq::calls":       "tsq_calls.qll",
		"tsq::variables":   "tsq_variables.qll",
		"tsq::expressions": "tsq_expressions.qll",
		"tsq::types":       "tsq_types.qll",
		"tsq::imports":     "tsq_imports.qll",
		"tsq::symbols":     "tsq_symbols.qll",
		"tsq::taint":       "tsq_taint.qll",
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

// TestCompatJavascriptParses verifies the compat file parses without errors.
func TestCompatJavascriptParses(t *testing.T) {
	files := LoadBridge()
	data, ok := files["compat_javascript.qll"]
	if !ok {
		t.Fatal("compat_javascript.qll not found in embedded bridge files")
	}
	p := parse.NewParser(string(data), "compat_javascript.qll")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(mod.Classes) == 0 {
		t.Error("compat file parsed but contains no class declarations")
	}
}

// TestCompatJavascriptRegistersClasses verifies that even though the compat
// file has resolve warnings about raw relation predicates (expected for bridge
// files), all expected class declarations are registered in the environment.
func TestCompatJavascriptRegistersClasses(t *testing.T) {
	files := LoadBridge()
	data, ok := files["compat_javascript.qll"]
	if !ok {
		t.Fatal("compat_javascript.qll not found")
	}
	p := parse.NewParser(string(data), "compat_javascript.qll")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	// Resolve standalone (will have expected errors for DB relation references).
	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("resolve returned fatal error: %v", err)
	}

	expectedClasses := []string{
		"ASTNode",
		"File",
		"Function",
		"CallExpr",
		"MethodCallExpr",
		"NewExpr",
		"VarDef",
		"VarAccess",
		"AssignExpr",
		"PropAccess",
		"PropWrite",
		"AwaitExpr",
		"ClassDefinition",
		"InterfaceDefinition",
		"TypeDefinition",
		"ImportDeclaration",
		"ExportDeclaration",
		"Parameter",
		"Symbol",
		"TaintSource",
		"TaintSink",
		"TaintAlert",
	}

	for _, name := range expectedClasses {
		if _, ok := rm.Env.Classes[name]; !ok {
			t.Errorf("compat file missing expected class %q", name)
		}
	}

	// Verify we got the right count.
	if got := len(rm.Env.Classes); got != len(expectedClasses) {
		t.Errorf("expected %d classes, got %d", len(expectedClasses), got)
	}
}

// TestImportJavascriptQueryParseResolveDesugar verifies that a query using
// `import javascript` with CodeQL-style class names parses, resolves, and
// desugars without errors end-to-end.
func TestImportJavascriptQueryParseResolveDesugar(t *testing.T) {
	query := `import javascript

from CallExpr call
select call
`
	p := parse.NewParser(query, "test_import_js.ql")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Verify the import was parsed correctly.
	if len(mod.Imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(mod.Imports))
	}
	if mod.Imports[0].Path != "javascript" {
		t.Fatalf("expected import path 'javascript', got %q", mod.Imports[0].Path)
	}

	// Resolve with the javascript import loader.
	loader := makeCompatImportLoader()
	rm, err := resolve.Resolve(mod, loader)
	if err != nil {
		t.Fatalf("resolve error: %v", err)
	}
	// The query itself should have zero resolve errors (imported bridge
	// module errors are internal and not propagated to the importing module).
	if len(rm.Errors) > 0 {
		for _, e := range rm.Errors {
			t.Errorf("resolve error: %v", e)
		}
		t.FailNow()
	}

	// Verify CallExpr class is available in the resolved environment.
	if _, ok := rm.Env.Classes["CallExpr"]; !ok {
		t.Error("CallExpr class not available after import javascript")
	}

	// Desugar.
	_, dsErrors := desugar.Desugar(rm)
	if len(dsErrors) > 0 {
		for _, e := range dsErrors {
			t.Errorf("desugar error: %v", e)
		}
	}
}

// TestImportJavascriptMethodCallQuery tests a query using MethodCallExpr
// with a predicate filter to verify member access on compat classes works.
func TestImportJavascriptMethodCallQuery(t *testing.T) {
	query := `import javascript

from MethodCallExpr mc
where mc.getMethodName() = "exec"
select mc
`
	p := parse.NewParser(query, "test_method_call.ql")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	loader := makeCompatImportLoader()
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

	// Verify MethodCallExpr has getMethodName member.
	mcClass, ok := rm.Env.Classes["MethodCallExpr"]
	if !ok {
		t.Fatal("MethodCallExpr class not available")
	}
	foundGetMethodName := false
	foundGetReceiver := false
	for _, m := range mcClass.Members {
		if m.Name == "getMethodName" {
			foundGetMethodName = true
		}
		if m.Name == "getReceiver" {
			foundGetReceiver = true
		}
	}
	if !foundGetMethodName {
		t.Error("MethodCallExpr class missing getMethodName member")
	}
	if !foundGetReceiver {
		t.Error("MethodCallExpr class missing getReceiver member")
	}

	_, dsErrors := desugar.Desugar(rm)
	if len(dsErrors) > 0 {
		for _, e := range dsErrors {
			t.Errorf("desugar error: %v", e)
		}
	}
}

// TestImportJavascriptAllClassesAvailable verifies that all compat classes
// are available in a query after `import javascript`.
func TestImportJavascriptAllClassesAvailable(t *testing.T) {
	query := `import javascript

from VarDef v
select v
`
	p := parse.NewParser(query, "test_all_classes.ql")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	loader := makeCompatImportLoader()
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

	// Verify all compat classes are available after import.
	for _, cls := range []string{
		"ASTNode", "File", "Function",
		"CallExpr", "MethodCallExpr", "NewExpr",
		"VarDef", "VarAccess", "AssignExpr",
		"PropAccess", "PropWrite", "AwaitExpr",
		"ClassDefinition", "InterfaceDefinition", "TypeDefinition",
		"ImportDeclaration", "ExportDeclaration",
		"Parameter", "Symbol",
		"TaintSource", "TaintSink", "TaintAlert",
	} {
		if _, ok := rm.Env.Classes[cls]; !ok {
			t.Errorf("class %q not available after import javascript", cls)
		}
	}
}

// TestImportJavascriptCallExprMembers verifies CallExpr has the expected
// member predicates with correct names.
func TestImportJavascriptCallExprMembers(t *testing.T) {
	files := LoadBridge()
	data := files["compat_javascript.qll"]
	p := parse.NewParser(string(data), "compat_javascript.qll")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	rm, _ := resolve.Resolve(mod, nil)

	cd, ok := rm.Env.Classes["CallExpr"]
	if !ok {
		t.Fatal("CallExpr class not found")
	}

	expectedMembers := map[string]bool{
		"getCallee":      false,
		"getNumArgument": false,
		"getArgument":    false,
		"getAnArgument":  false,
		"toString":       false,
	}

	for _, m := range cd.Members {
		if _, want := expectedMembers[m.Name]; want {
			expectedMembers[m.Name] = true
		}
	}

	for name, found := range expectedMembers {
		if !found {
			t.Errorf("CallExpr missing expected member %q", name)
		}
	}
}
