package extract

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
)

// vendoredTestdataDir returns the absolute path to testdata/ts/vendored/.
func vendoredTestdataDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "testdata", "ts", "vendored")
}

func newOpenVendoredBackend(t *testing.T, rootDir string) *VendoredBackend {
	t.Helper()
	b := &VendoredBackend{}
	ctx := context.Background()
	if err := b.Open(ctx, ProjectConfig{RootDir: rootDir}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

// TestVendoredBackend_Open_FindsFiles checks that Open resolves .ts files.
func TestVendoredBackend_Open_FindsFiles(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)

	if b.treeSitter == nil {
		t.Fatal("tree-sitter backend not initialised")
	}
	if len(b.treeSitter.files) == 0 {
		t.Fatal("expected at least one source file, got none")
	}
}

// TestVendoredBackend_Open_SetsRootDir checks rootDir is set correctly.
func TestVendoredBackend_Open_SetsRootDir(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)

	if b.rootDir != dir {
		t.Errorf("rootDir = %q, want %q", b.rootDir, dir)
	}
}

// TestVendoredBackend_WalkAST_VisitorCalled checks that the visitor receives calls.
func TestVendoredBackend_WalkAST_VisitorCalled(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)
	v := newCollectingVisitor()

	if err := b.WalkAST(context.Background(), v); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}

	if len(v.files) == 0 {
		t.Fatal("no files visited")
	}
	if len(v.allKinds) == 0 {
		t.Fatal("no nodes visited")
	}
}

// TestVendoredBackend_WalkAST_FunctionDeclaration checks that simple.ts yields
// FunctionDeclaration nodes.
func TestVendoredBackend_WalkAST_FunctionDeclaration(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)
	v := newCollectingVisitor()

	if err := b.WalkAST(context.Background(), v); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}

	if !v.hasKind("FunctionDeclaration") {
		t.Error("expected FunctionDeclaration nodes, got none")
	}
}

// TestVendoredBackend_WalkAST_ArrowFunction checks that arrow.ts yields
// ArrowFunction nodes.
func TestVendoredBackend_WalkAST_ArrowFunction(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)
	v := newCollectingVisitor()

	if err := b.WalkAST(context.Background(), v); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}

	if !v.hasKind("ArrowFunction") {
		t.Error("expected ArrowFunction nodes, got none")
	}
}

// TestVendoredBackend_WalkAST_Identifier checks that Identifier nodes are found.
func TestVendoredBackend_WalkAST_Identifier(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)
	v := newCollectingVisitor()

	if err := b.WalkAST(context.Background(), v); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}

	if !v.hasKind("Identifier") {
		t.Error("expected Identifier nodes, got none")
	}
}

// TestVendoredBackend_WalkAST_CallExpression checks that CallExpression nodes
// are found in arrow.ts (result = add(double(3), 4)).
func TestVendoredBackend_WalkAST_CallExpression(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)
	v := newCollectingVisitor()

	if err := b.WalkAST(context.Background(), v); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}

	if !v.hasKind("CallExpression") {
		t.Error("expected CallExpression nodes, got none")
	}
}

// TestVendoredBackend_WalkAST_NodePositions verifies position values are sensible.
func TestVendoredBackend_WalkAST_NodePositions(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)

	pv := &funcVisitor{
		enterFn: func(node ASTNode) (bool, error) {
			if node.StartLine() < 1 {
				t.Errorf("node %s: StartLine %d < 1", node.Kind(), node.StartLine())
			}
			if node.EndLine() < node.StartLine() {
				t.Errorf("node %s: EndLine %d < StartLine %d", node.Kind(), node.EndLine(), node.StartLine())
			}
			return true, nil
		},
	}

	if err := b.WalkAST(context.Background(), pv); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}
}

// TestVendoredBackend_WalkAST_DescendFalse checks that descend=false skips children.
func TestVendoredBackend_WalkAST_DescendFalse(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)

	count := 0
	pv := &funcVisitor{
		enterFn: func(node ASTNode) (bool, error) {
			count++
			return false, nil
		},
	}

	if err := b.WalkAST(context.Background(), pv); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}

	fileCount := len(b.treeSitter.files)
	if count != fileCount {
		t.Errorf("expected %d nodes (one root per file), got %d", fileCount, count)
	}
}

// TestVendoredBackend_WalkAST_VisitorError checks that an error from Enter aborts.
func TestVendoredBackend_WalkAST_VisitorError(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)

	sentinel := errors.New("stop walking")
	count := 0
	pv := &funcVisitor{
		enterFn: func(node ASTNode) (bool, error) {
			count++
			if count >= 3 {
				return false, sentinel
			}
			return true, nil
		},
	}

	err := b.WalkAST(context.Background(), pv)
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got: %v", err)
	}
}

// TestVendoredBackend_WalkAST_ContextCancel checks context cancellation.
func TestVendoredBackend_WalkAST_ContextCancel(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)

	ctx, cancel := context.WithCancel(context.Background())

	count := 0
	pv := &funcVisitor{
		enterFileFn: func(path string) error {
			count++
			if count >= 1 {
				cancel()
			}
			return nil
		},
		enterFn: func(node ASTNode) (bool, error) {
			return true, nil
		},
	}

	err := b.WalkAST(ctx, pv)
	if err == nil && len(b.treeSitter.files) > 1 {
		t.Error("expected walk to be cancelled but it completed")
	}
}

// TestVendoredBackend_SemanticMethods_NoTsgo checks that semantic methods return
// ErrUnsupported when tsgo is not available (expected in CI/test environment).
func TestVendoredBackend_SemanticMethods_NoTsgo(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)
	ctx := context.Background()

	// tsgo is almost certainly not installed in the test environment.
	if b.TsgoAvailable() {
		t.Skip("tsgo is available -- skipping degraded-mode tests")
	}

	_, err := b.ResolveSymbol(ctx, SymbolRef{FilePath: "test.ts", Name: "x"})
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("ResolveSymbol: expected ErrUnsupported, got %v", err)
	}

	_, err = b.ResolveType(ctx, NodeRef{FilePath: "test.ts"})
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("ResolveType: expected ErrUnsupported, got %v", err)
	}

	_, err = b.CrossFileRefs(ctx, SymbolRef{FilePath: "test.ts", Name: "x"})
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("CrossFileRefs: expected ErrUnsupported, got %v", err)
	}
}

// TestVendoredBackend_Close_Idempotent checks double-close doesn't panic.
func TestVendoredBackend_Close_Idempotent(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := &VendoredBackend{}
	if err := b.Open(context.Background(), ProjectConfig{RootDir: dir}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// TestVendoredBackend_TsgoAvailable_False checks TsgoAvailable when tsgo is absent.
func TestVendoredBackend_TsgoAvailable_False(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)

	// In the test environment tsgo is almost certainly not installed.
	// This test documents the degraded state.
	if b.TsgoAvailable() {
		t.Skip("tsgo is available; cannot test unavailable state")
	}
	// Confirm the backend is still usable for AST walking.
	v := newCollectingVisitor()
	if err := b.WalkAST(context.Background(), v); err != nil {
		t.Fatalf("WalkAST should work without tsgo: %v", err)
	}
	if len(v.allKinds) == 0 {
		t.Error("expected nodes from WalkAST even without tsgo")
	}
}

// TestVendoredBackend_WalkAST_ChildCount checks child consistency.
func TestVendoredBackend_WalkAST_ChildCount(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)

	pv := &funcVisitor{
		enterFn: func(node ASTNode) (bool, error) {
			count := node.ChildCount()
			for i := 0; i < count; i++ {
				child := node.Child(i)
				if child == nil {
					t.Errorf("node %s: Child(%d) returned nil but ChildCount=%d",
						node.Kind(), i, count)
				}
			}
			return true, nil
		},
	}

	if err := b.WalkAST(context.Background(), pv); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}
}

// TestVendoredBackend_WalkAST_FieldNames checks that some nodes have field names.
func TestVendoredBackend_WalkAST_FieldNames(t *testing.T) {
	dir := vendoredTestdataDir(t)
	b := newOpenVendoredBackend(t, dir)

	var namedFields []string
	pv := &funcVisitor{
		enterFn: func(node ASTNode) (bool, error) {
			if fn := node.FieldName(); fn != "" {
				namedFields = append(namedFields, fn)
			}
			return true, nil
		},
	}

	if err := b.WalkAST(context.Background(), pv); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}

	if len(namedFields) == 0 {
		t.Error("expected some nodes with field names, got none")
	}
}
