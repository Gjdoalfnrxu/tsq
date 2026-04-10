package bridge

import (
	"fmt"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/ast"
	"github.com/Gjdoalfnrxu/tsq/ql/desugar"
	"github.com/Gjdoalfnrxu/tsq/ql/parse"
	"github.com/Gjdoalfnrxu/tsq/ql/resolve"
)

// makeSecurityImportLoader creates an import loader that resolves security
// query imports (and all tsq:: paths) to the appropriate bridge file.
func makeSecurityImportLoader() func(string) (*ast.Module, error) {
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
		"semmle.javascript.security.dataflow.XssQuery":              "compat_security_xss.qll",
		"semmle.javascript.security.dataflow.CommandInjectionQuery": "compat_security_cmdi.qll",
		"semmle.javascript.security.dataflow.SqlInjectionQuery":     "compat_security_sqli.qll",
		"semmle.javascript.security.dataflow.PathTraversalQuery":    "compat_security_pathtraversal.qll",
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

// TestSecurityQllFilesParseWithoutErrors verifies all 4 security .qll files
// parse without errors.
func TestSecurityQllFilesParseWithoutErrors(t *testing.T) {
	files := LoadBridge()
	securityFiles := []string{
		"compat_security_xss.qll",
		"compat_security_cmdi.qll",
		"compat_security_sqli.qll",
		"compat_security_pathtraversal.qll",
	}
	for _, name := range securityFiles {
		data, ok := files[name]
		if !ok {
			t.Errorf("%s not found in embedded bridge files", name)
			continue
		}
		p := parse.NewParser(string(data), name)
		mod, err := p.Parse()
		if err != nil {
			t.Errorf("%s parse error: %v", name, err)
			continue
		}
		if len(mod.Classes) == 0 && len(mod.Modules) == 0 {
			t.Errorf("%s parsed but contains no classes or modules", name)
		}
	}
}

// TestSecurityXssModuleClassesRegistered verifies XSS module classes are
// registered after resolve.
func TestSecurityXssModuleClassesRegistered(t *testing.T) {
	files := LoadBridge()
	data := files["compat_security_xss.qll"]
	p := parse.NewParser(string(data), "compat_security_xss.qll")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("resolve error: %v", err)
	}

	for _, name := range []string{"Xss::XssSource", "Xss::XssSink"} {
		if _, ok := rm.Env.Classes[name]; !ok {
			t.Errorf("XSS module missing expected class %q", name)
		}
	}
}

// TestSecurityCmdiModuleClassesRegistered verifies command injection module
// classes are registered after resolve.
func TestSecurityCmdiModuleClassesRegistered(t *testing.T) {
	files := LoadBridge()
	data := files["compat_security_cmdi.qll"]
	p := parse.NewParser(string(data), "compat_security_cmdi.qll")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("resolve error: %v", err)
	}

	for _, name := range []string{"CommandInjection::CommandInjectionSource", "CommandInjection::CommandInjectionSink"} {
		if _, ok := rm.Env.Classes[name]; !ok {
			t.Errorf("CommandInjection module missing expected class %q", name)
		}
	}
}

// TestSecuritySqliModuleClassesRegistered verifies SQL injection module
// classes are registered after resolve.
func TestSecuritySqliModuleClassesRegistered(t *testing.T) {
	files := LoadBridge()
	data := files["compat_security_sqli.qll"]
	p := parse.NewParser(string(data), "compat_security_sqli.qll")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("resolve error: %v", err)
	}

	for _, name := range []string{"SqlInjection::SqlInjectionSource", "SqlInjection::SqlInjectionSink"} {
		if _, ok := rm.Env.Classes[name]; !ok {
			t.Errorf("SqlInjection module missing expected class %q", name)
		}
	}
}

// TestSecurityPathTraversalModuleClassesRegistered verifies path traversal
// module classes are registered after resolve.
func TestSecurityPathTraversalModuleClassesRegistered(t *testing.T) {
	files := LoadBridge()
	data := files["compat_security_pathtraversal.qll"]
	p := parse.NewParser(string(data), "compat_security_pathtraversal.qll")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	rm, err := resolve.Resolve(mod, nil)
	if err != nil {
		t.Fatalf("resolve error: %v", err)
	}

	for _, name := range []string{"PathTraversal::PathTraversalSource", "PathTraversal::PathTraversalSink"} {
		if _, ok := rm.Env.Classes[name]; !ok {
			t.Errorf("PathTraversal module missing expected class %q", name)
		}
	}
}

// TestSecurityXssEndToEndQuery tests an end-to-end query that imports the
// XSS security library and selects from Xss::XssSource.
func TestSecurityXssEndToEndQuery(t *testing.T) {
	query := `import semmle.javascript.security.dataflow.XssQuery

from Xss::XssSource src
select src
`
	p := parse.NewParser(query, "test_xss_query.ql")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(mod.Imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(mod.Imports))
	}
	if mod.Imports[0].Path != "semmle.javascript.security.dataflow.XssQuery" {
		t.Fatalf("expected import path 'semmle.javascript.security.dataflow.XssQuery', got %q", mod.Imports[0].Path)
	}

	loader := makeSecurityImportLoader()
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

	// Verify Xss::XssSource class is available.
	if _, ok := rm.Env.Classes["Xss::XssSource"]; !ok {
		t.Error("Xss::XssSource class not available after import XssQuery")
	}

	// Desugar should succeed.
	_, dsErrors := desugar.Desugar(rm)
	if len(dsErrors) > 0 {
		for _, e := range dsErrors {
			t.Errorf("desugar error: %v", e)
		}
	}
}

// TestSecurityCmdiEndToEndQuery tests an end-to-end query that imports the
// command injection security library.
func TestSecurityCmdiEndToEndQuery(t *testing.T) {
	query := `import semmle.javascript.security.dataflow.CommandInjectionQuery

from CommandInjection::CommandInjectionSink sink
select sink
`
	p := parse.NewParser(query, "test_cmdi_query.ql")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	loader := makeSecurityImportLoader()
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

	if _, ok := rm.Env.Classes["CommandInjection::CommandInjectionSink"]; !ok {
		t.Error("CommandInjection::CommandInjectionSink class not available after import CommandInjectionQuery")
	}

	_, dsErrors := desugar.Desugar(rm)
	if len(dsErrors) > 0 {
		for _, e := range dsErrors {
			t.Errorf("desugar error: %v", e)
		}
	}
}

// TestSecuritySqliEndToEndQuery tests an end-to-end query that imports the
// SQL injection security library.
func TestSecuritySqliEndToEndQuery(t *testing.T) {
	query := `import semmle.javascript.security.dataflow.SqlInjectionQuery

from SqlInjection::SqlInjectionSource src
select src
`
	p := parse.NewParser(query, "test_sqli_query.ql")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	loader := makeSecurityImportLoader()
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

	if _, ok := rm.Env.Classes["SqlInjection::SqlInjectionSource"]; !ok {
		t.Error("SqlInjection::SqlInjectionSource class not available after import SqlInjectionQuery")
	}

	_, dsErrors := desugar.Desugar(rm)
	if len(dsErrors) > 0 {
		for _, e := range dsErrors {
			t.Errorf("desugar error: %v", e)
		}
	}
}

// TestSecurityPathTraversalEndToEndQuery tests an end-to-end query that
// imports the path traversal security library.
func TestSecurityPathTraversalEndToEndQuery(t *testing.T) {
	query := `import semmle.javascript.security.dataflow.PathTraversalQuery

from PathTraversal::PathTraversalSink sink
select sink
`
	p := parse.NewParser(query, "test_pathtraversal_query.ql")
	mod, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	loader := makeSecurityImportLoader()
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

	if _, ok := rm.Env.Classes["PathTraversal::PathTraversalSink"]; !ok {
		t.Error("PathTraversal::PathTraversalSink class not available after import PathTraversalQuery")
	}

	_, dsErrors := desugar.Desugar(rm)
	if len(dsErrors) > 0 {
		for _, e := range dsErrors {
			t.Errorf("desugar error: %v", e)
		}
	}
}
