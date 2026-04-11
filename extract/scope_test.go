package extract

import (
	"context"
	"path/filepath"
	"testing"
)

// buildScopeForFile walks the given file and builds a ScopeAnalyzer for it.
// The scope is built while the tree is still open (nodes are valid).
// The returned Scope is valid after WalkAST returns because it only stores
// byte positions and strings — not pointers to sitter.Node.
func buildScopeForFile(t *testing.T, filename string) (*ScopeAnalyzer, *Scope) {
	t.Helper()
	dir := testdataDir(t)
	path := filepath.Join(dir, filename)

	b := &TreeSitterBackend{}
	ctx := context.Background()
	if err := b.Open(ctx, ProjectConfig{RootDir: dir}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer b.Close()

	sa := NewScopeAnalyzer(path)
	var fileScope *Scope

	// We build scope inside a custom walk that processes each file's root.
	// We use a treeCapturingVisitor that collects the root node and
	// builds the scope while the tree is alive.
	type captureState struct {
		inTarget bool
		root     ASTNode
		built    bool
	}
	state := &captureState{}

	pv := &funcVisitor{
		enterFileFn: func(p string) error {
			state.inTarget = (p == path)
			state.root = nil
			state.built = false
			return nil
		},
		enterFn: func(node ASTNode) (bool, error) {
			if !state.inTarget {
				return false, nil
			}
			// Capture root (first node = program node)
			if state.root == nil {
				state.root = node
				// Build scope NOW while the tree is alive
				fileScope = sa.Build(node)
				state.built = true
				return true, nil
			}
			return true, nil
		},
		leaveFileFn: func(p string) error {
			return nil
		},
	}

	if err := b.WalkAST(ctx, pv); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}

	if fileScope == nil {
		t.Fatalf("did not build scope for %s", filename)
	}
	return sa, fileScope
}

// TestScopeAnalyzer_Build_SimpleFunction checks that function names are
// declared at file scope.
func TestScopeAnalyzer_Build_SimpleFunction(t *testing.T) {
	_, fileScope := buildScopeForFile(t, "simple_function.ts")

	// greet should be declared at file scope
	if _, ok := fileScope.decls["greet"]; !ok {
		t.Error("expected 'greet' to be declared at file scope")
	}

	// add should be declared at file scope
	if _, ok := fileScope.decls["add"]; !ok {
		t.Error("expected 'add' to be declared at file scope")
	}

	// outer should be declared at file scope
	if _, ok := fileScope.decls["outer"]; !ok {
		t.Error("expected 'outer' to be declared at file scope")
	}

	// result (const) should be at file scope
	if _, ok := fileScope.decls["result"]; !ok {
		t.Error("expected 'result' const to be declared at file scope")
	}
}

// TestScopeAnalyzer_Resolve_FileScope checks that file-level declarations
// are resolvable at byte offset 0.
func TestScopeAnalyzer_Resolve_FileScope(t *testing.T) {
	_, fileScope := buildScopeForFile(t, "simple_function.ts")

	// "greet" should resolve from any byte position
	decl, ok := fileScope.Resolve("greet", 0)
	if !ok {
		t.Error("expected to resolve 'greet' from file scope at byte 0")
	}
	if ok && decl.Name != "greet" {
		t.Errorf("resolved name mismatch: got %q", decl.Name)
	}

	// Nonexistent name should not resolve
	_, ok = fileScope.Resolve("doesNotExist", 0)
	if ok {
		t.Error("expected 'doesNotExist' not to resolve")
	}
}

// TestScopeAnalyzer_Imports checks that import bindings are declared.
func TestScopeAnalyzer_Imports(t *testing.T) {
	_, fileScope := buildScopeForFile(t, "imports.ts")

	// Check for default import
	if _, ok := fileScope.decls["defaultExport"]; !ok {
		t.Error("expected 'defaultExport' to be declared via import")
	}

	// Check for named imports
	if _, ok := fileScope.decls["named1"]; !ok {
		t.Error("expected 'named1' to be declared via import")
	}
	if _, ok := fileScope.decls["named2"]; !ok {
		t.Error("expected 'named2' to be declared via import")
	}

	// Check for aliased import: "import { original as alias } from ..."
	if _, ok := fileScope.decls["alias"]; !ok {
		t.Error("expected 'alias' to be declared via aliased import")
	}

	// Check for namespace import: "import * as namespace from ..."
	if _, ok := fileScope.decls["namespace"]; !ok {
		t.Error("expected 'namespace' to be declared via namespace import")
	}
}

// TestScopeAnalyzer_TDZ_DirectCheck verifies TDZ logic directly on Scope.Resolve.
func TestScopeAnalyzer_TDZ_DirectCheck(t *testing.T) {
	s := newScope(nil)
	s.declare("letVar", &Declaration{
		Name:      "letVar",
		FilePath:  "test.ts",
		StartByte: 100,
		isTDZ:     true, // let/const
	})

	// Before declaration: TDZ — should not resolve
	_, ok := s.Resolve("letVar", 50)
	if ok {
		t.Error("expected TDZ: 'letVar' should not resolve before byte 100")
	}

	// Exactly at declaration: accessible
	decl, ok := s.Resolve("letVar", 100)
	if !ok {
		t.Error("expected 'letVar' to resolve at its declaration byte")
	}
	_ = decl

	// After declaration: accessible
	decl, ok = s.Resolve("letVar", 200)
	if !ok {
		t.Error("expected 'letVar' to resolve after its declaration byte")
	}
	_ = decl
}

// TestScopeAnalyzer_VarNoTDZ verifies that var declarations have no TDZ.
func TestScopeAnalyzer_VarNoTDZ(t *testing.T) {
	s := newScope(nil)
	s.declare("varX", &Declaration{
		Name:      "varX",
		FilePath:  "test.ts",
		StartByte: 100,
		isTDZ:     false, // var — no TDZ
	})

	// Before declaration: var is accessible (hoisted — no TDZ)
	decl, ok := s.Resolve("varX", 0)
	if !ok {
		t.Error("expected var 'varX' to resolve before its declaration (no TDZ)")
	}
	_ = decl
}

// TestScopeAnalyzer_Resolve_UnknownName checks that Resolve returns false
// for unknown names.
func TestScopeAnalyzer_Resolve_UnknownName(t *testing.T) {
	s := newScope(nil)
	_, ok := s.Resolve("nonexistent", 0)
	if ok {
		t.Error("expected Resolve to return false for unknown name")
	}
}

// TestScopeAnalyzer_Resolve_ParentScope checks that a child scope can
// resolve names from its parent.
func TestScopeAnalyzer_Resolve_ParentScope(t *testing.T) {
	parent := newScope(nil)
	parent.declare("outerVar", &Declaration{
		Name:      "outerVar",
		FilePath:  "test.ts",
		StartByte: 0,
		isTDZ:     false,
	})

	child := newScope(parent)
	child.declare("innerVar", &Declaration{
		Name:      "innerVar",
		FilePath:  "test.ts",
		StartByte: 10,
		isTDZ:     false,
	})

	// Child can see outerVar
	decl, ok := child.Resolve("outerVar", 5)
	if !ok {
		t.Error("expected child to resolve 'outerVar' from parent scope")
	}
	if ok && decl.Name != "outerVar" {
		t.Errorf("resolved wrong declaration: %q", decl.Name)
	}

	// Parent cannot see innerVar
	_, ok = parent.Resolve("innerVar", 15)
	if ok {
		t.Error("expected parent NOT to resolve 'innerVar' (child-only)")
	}

	// Child can see innerVar
	decl, ok = child.Resolve("innerVar", 15)
	if !ok {
		t.Error("expected child to resolve 'innerVar'")
	}
	if ok && decl.Name != "innerVar" {
		t.Errorf("resolved wrong declaration: %q", decl.Name)
	}
}

// TestScopeAnalyzer_Shadowing_ChildOverridesParent checks that a child
// declaration shadows the parent one at the resolution site.
func TestScopeAnalyzer_Shadowing_ChildOverridesParent(t *testing.T) {
	parent := newScope(nil)
	parent.declare("x", &Declaration{
		Name:      "x",
		FilePath:  "test.ts",
		StartByte: 0,
		isTDZ:     false,
	})

	child := newScope(parent)
	child.declare("x", &Declaration{
		Name:      "x",
		FilePath:  "test.ts",
		StartByte: 50,
		isTDZ:     false,
	})

	decl, ok := child.Resolve("x", 100)
	if !ok {
		t.Fatal("expected 'x' to resolve in child scope")
	}
	// Should get the child's declaration (StartByte 50), not parent's (0)
	if decl.StartByte != 50 {
		t.Errorf("expected child's declaration (StartByte=50), got StartByte=%d", decl.StartByte)
	}
}

// TestScopeAnalyzer_Scoping_VarAtFileScope checks that var in scoping.ts
// is visible at file scope.
func TestScopeAnalyzer_Scoping_VarAtFileScope(t *testing.T) {
	_, fileScope := buildScopeForFile(t, "scoping.ts")

	// "x" declared with var at file level
	decl, ok := fileScope.Resolve("x", 0)
	if !ok {
		t.Error("expected 'x' (var) to resolve from byte 0")
	}
	if ok && decl.isTDZ {
		t.Error("var 'x' should not have TDZ flag")
	}
}

// TestScopeAnalyzer_ArrowFunction checks that arrow functions bound to consts
// are in scope at file level.
func TestScopeAnalyzer_ArrowFunction(t *testing.T) {
	_, fileScope := buildScopeForFile(t, "arrow_functions.ts")

	// File-level arrow function declarations using const
	if _, ok := fileScope.decls["double"]; !ok {
		t.Error("expected 'double' to be declared at file scope")
	}
	if _, ok := fileScope.decls["add"]; !ok {
		t.Error("expected 'add' to be declared at file scope")
	}
	if _, ok := fileScope.decls["noArgs"]; !ok {
		t.Error("expected 'noArgs' to be declared at file scope")
	}
}

// TestScopeAnalyzer_SyntaxError_DoesNotPanic checks that building scope
// on a file with syntax errors does not panic.
func TestScopeAnalyzer_SyntaxError_DoesNotPanic(t *testing.T) {
	// Should not panic or crash — tree-sitter produces ERROR nodes but
	// the scope analyzer should handle them gracefully via the default case.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("scope analysis panicked on syntax_error.ts: %v", r)
		}
	}()
	_, fileScope := buildScopeForFile(t, "syntax_error.ts")
	// "valid" function should still be declared despite errors elsewhere
	if _, ok := fileScope.decls["valid"]; !ok {
		t.Error("expected 'valid' to be declared despite syntax errors")
	}
}

// TestScopeAnalyzer_TDZ_FromFile checks TDZ from an actual scoped file.
func TestScopeAnalyzer_TDZ_FromFile(t *testing.T) {
	_, fileScope := buildScopeForFile(t, "scoping.ts")

	// Find a let/const declaration and verify TDZ applies
	var foundTDZ bool
	for name, d := range fileScope.decls {
		if d.isTDZ && d.StartByte > 0 {
			// Should NOT resolve before its declaration
			_, ok := fileScope.Resolve(name, 0)
			if ok {
				t.Errorf("let/const %q resolved before its declaration (byte %d) — TDZ violated", name, d.StartByte)
			}
			// Should resolve after
			_, ok = fileScope.Resolve(name, d.StartByte+1)
			if !ok {
				t.Errorf("let/const %q did not resolve after declaration byte %d", name, d.StartByte)
			}
			foundTDZ = true
			break
		}
	}
	if !foundTDZ {
		t.Fatalf("expected at least one TDZ declaration at file scope in scoping.ts, found none — TDZ analysis has regressed or the fixture is broken")
	}
}
