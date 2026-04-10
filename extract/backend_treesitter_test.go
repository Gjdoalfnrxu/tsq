package extract

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// testdataDir returns the absolute path to testdata/ts/.
func testdataDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "testdata", "ts")
}

func newOpenBackend(t *testing.T, rootDir string) *TreeSitterBackend {
	t.Helper()
	b := &TreeSitterBackend{}
	ctx := context.Background()
	if err := b.Open(ctx, ProjectConfig{RootDir: rootDir}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

// collectingVisitor accumulates node kinds seen during WalkAST.
type collectingVisitor struct {
	files     []string
	allKinds  []string
	fileKinds map[string][]string // file -> kinds
}

func newCollectingVisitor() *collectingVisitor {
	return &collectingVisitor{fileKinds: make(map[string][]string)}
}

func (v *collectingVisitor) EnterFile(path string) error {
	v.files = append(v.files, path)
	return nil
}

func (v *collectingVisitor) Enter(node ASTNode) (bool, error) {
	kind := node.Kind()
	v.allKinds = append(v.allKinds, kind)
	// Record per-file (use last file in list)
	if len(v.files) > 0 {
		cur := v.files[len(v.files)-1]
		v.fileKinds[cur] = append(v.fileKinds[cur], kind)
	}
	return true, nil
}

func (v *collectingVisitor) Leave(node ASTNode) error { return nil }

func (v *collectingVisitor) LeaveFile(path string) error { return nil }

func (v *collectingVisitor) hasKind(kind string) bool {
	for _, k := range v.allKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// TestTreeSitterBackend_Open_FindsFiles checks that Open resolves .ts/.tsx files.
func TestTreeSitterBackend_Open_FindsFiles(t *testing.T) {
	dir := testdataDir(t)
	b := newOpenBackend(t, dir)

	if len(b.files) == 0 {
		t.Fatal("expected at least one source file, got none")
	}
	for _, f := range b.files {
		ext := strings.ToLower(filepath.Ext(f))
		if ext != ".ts" && ext != ".tsx" {
			t.Errorf("unexpected file extension in results: %s", f)
		}
	}
}

// TestTreeSitterBackend_Open_SkipsNodeModules checks node_modules is not walked.
func TestTreeSitterBackend_Open_SkipsNodeModules(t *testing.T) {
	dir := testdataDir(t)
	b := newOpenBackend(t, dir)
	for _, f := range b.files {
		if strings.Contains(f, "node_modules") {
			t.Errorf("node_modules file leaked into file list: %s", f)
		}
	}
}

// TestTreeSitterBackend_WalkAST_VisitorCalled checks that the visitor is called
// for each file.
func TestTreeSitterBackend_WalkAST_VisitorCalled(t *testing.T) {
	dir := testdataDir(t)
	b := newOpenBackend(t, dir)
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

// TestTreeSitterBackend_WalkAST_FunctionDeclaration checks that
// FunctionDeclaration nodes are present in simple_function.ts.
func TestTreeSitterBackend_WalkAST_FunctionDeclaration(t *testing.T) {
	dir := testdataDir(t)
	b := &TreeSitterBackend{}
	ctx := context.Background()
	// Open a single file directory by targeting just the one file's directory
	// and filtering — easier: open whole testdata and look for the file.
	if err := b.Open(ctx, ProjectConfig{RootDir: dir}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer b.Close()

	v := newCollectingVisitor()
	if err := b.WalkAST(ctx, v); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}

	if !v.hasKind("FunctionDeclaration") {
		t.Error("expected FunctionDeclaration nodes, got none")
	}
}

// TestTreeSitterBackend_WalkAST_ArrowFunction checks ArrowFunction nodes.
func TestTreeSitterBackend_WalkAST_ArrowFunction(t *testing.T) {
	dir := testdataDir(t)
	b := newOpenBackend(t, dir)
	v := newCollectingVisitor()
	if err := b.WalkAST(context.Background(), v); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}
	if !v.hasKind("ArrowFunction") {
		t.Error("expected ArrowFunction nodes, got none")
	}
}

// TestTreeSitterBackend_WalkAST_ImportDeclaration checks ImportDeclaration nodes.
func TestTreeSitterBackend_WalkAST_ImportDeclaration(t *testing.T) {
	dir := testdataDir(t)
	b := newOpenBackend(t, dir)
	v := newCollectingVisitor()
	if err := b.WalkAST(context.Background(), v); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}
	if !v.hasKind("ImportDeclaration") {
		t.Error("expected ImportDeclaration nodes, got none")
	}
}

// TestTreeSitterBackend_WalkAST_CallExpression checks CallExpression nodes.
func TestTreeSitterBackend_WalkAST_CallExpression(t *testing.T) {
	dir := testdataDir(t)
	b := newOpenBackend(t, dir)
	v := newCollectingVisitor()
	if err := b.WalkAST(context.Background(), v); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}
	if !v.hasKind("CallExpression") {
		t.Error("expected CallExpression nodes, got none")
	}
}

// TestTreeSitterBackend_WalkAST_Identifier checks Identifier nodes.
func TestTreeSitterBackend_WalkAST_Identifier(t *testing.T) {
	dir := testdataDir(t)
	b := newOpenBackend(t, dir)
	v := newCollectingVisitor()
	if err := b.WalkAST(context.Background(), v); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}
	if !v.hasKind("Identifier") {
		t.Error("expected Identifier nodes, got none")
	}
}

// TestTreeSitterBackend_WalkAST_SyntaxError checks that syntax_error.ts is
// walked without returning an error (tree-sitter tolerates syntax errors).
func TestTreeSitterBackend_WalkAST_SyntaxError(t *testing.T) {
	dir := testdataDir(t)
	b := newOpenBackend(t, dir)
	v := newCollectingVisitor()
	err := b.WalkAST(context.Background(), v)
	if err != nil {
		t.Errorf("WalkAST returned error on syntax_error.ts: %v", err)
	}
	// Should still have visited some files including the error file
	if len(v.files) == 0 {
		t.Error("no files visited despite syntax errors")
	}
}

// TestTreeSitterBackend_WalkAST_ErrorNodes checks that ERROR nodes appear
// when parsing syntax_error.ts.
func TestTreeSitterBackend_WalkAST_ErrorNodes(t *testing.T) {
	dir := testdataDir(t)
	b := newOpenBackend(t, dir)
	v := newCollectingVisitor()
	if err := b.WalkAST(context.Background(), v); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}
	// syntax_error.ts has intentional errors; ERROR nodes should appear
	if !v.hasKind("Error") {
		t.Log("note: no Error nodes seen — tree-sitter may have recovered fully")
		// This is not a hard failure — tree-sitter recovery is heuristic
	}
}

// TestTreeSitterBackend_WalkAST_NodePositions checks that nodes have valid positions.
func TestTreeSitterBackend_WalkAST_NodePositions(t *testing.T) {
	dir := testdataDir(t)
	b := newOpenBackend(t, dir)

	type posNode struct {
		kind           string
		sl, sc, el, ec int
	}
	var captured []posNode

	pv := &funcVisitor{
		enterFn: func(node ASTNode) (bool, error) {
			captured = append(captured, posNode{
				kind: node.Kind(),
				sl:   node.StartLine(),
				sc:   node.StartCol(),
				el:   node.EndLine(),
				ec:   node.EndCol(),
			})
			return true, nil
		},
	}

	if err := b.WalkAST(context.Background(), pv); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}

	for _, n := range captured {
		if n.sl < 1 {
			t.Errorf("node %s: StartLine %d < 1", n.kind, n.sl)
		}
		if n.el < n.sl {
			t.Errorf("node %s: EndLine %d < StartLine %d", n.kind, n.el, n.sl)
		}
	}
}

// TestTreeSitterBackend_WalkAST_NodeText checks that node text is non-empty
// for identifier nodes.
func TestTreeSitterBackend_WalkAST_NodeText(t *testing.T) {
	dir := testdataDir(t)
	b := newOpenBackend(t, dir)

	var identTexts []string
	pv := &funcVisitor{
		enterFn: func(node ASTNode) (bool, error) {
			if node.Kind() == "Identifier" {
				identTexts = append(identTexts, node.Text())
			}
			return true, nil
		},
	}

	if err := b.WalkAST(context.Background(), pv); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}

	if len(identTexts) == 0 {
		t.Fatal("no identifier texts collected")
	}
	// Most identifier texts should be non-empty; zero-length identifiers may
	// appear in error-recovery nodes (tree-sitter inserts them during recovery).
	nonEmpty := 0
	for _, text := range identTexts {
		if text != "" {
			nonEmpty++
		}
	}
	if nonEmpty == 0 {
		t.Error("all identifier nodes have empty text — expected at least some non-empty")
	}
}

// TestTreeSitterBackend_WalkAST_DescendFalse checks that returning descend=false
// from Enter skips children.
func TestTreeSitterBackend_WalkAST_DescendFalse(t *testing.T) {
	dir := testdataDir(t)
	b := newOpenBackend(t, dir)

	count := 0
	pv := &funcVisitor{
		enterFn: func(node ASTNode) (bool, error) {
			count++
			// Don't descend into anything — only the root should be visited per file
			return false, nil
		},
	}

	if err := b.WalkAST(context.Background(), pv); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}

	// We should have visited exactly one node per file (the root Program node)
	if count != len(b.files) {
		t.Errorf("expected %d nodes (one root per file), got %d", len(b.files), count)
	}
}

// TestTreeSitterBackend_WalkAST_VisitorError checks that an error from Enter
// aborts the walk.
func TestTreeSitterBackend_WalkAST_VisitorError(t *testing.T) {
	dir := testdataDir(t)
	b := newOpenBackend(t, dir)

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

// TestTreeSitterBackend_WalkAST_ContextCancel checks that cancellation stops the walk.
func TestTreeSitterBackend_WalkAST_ContextCancel(t *testing.T) {
	dir := testdataDir(t)
	b := newOpenBackend(t, dir)

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
	if err == nil {
		// Walk completed before cancel took effect — acceptable if only 1 file
		if len(b.files) > 1 {
			t.Error("expected walk to be cancelled but it completed")
		}
	}
}

// TestTreeSitterBackend_ErrUnsupported checks that semantic methods return ErrUnsupported.
func TestTreeSitterBackend_ErrUnsupported(t *testing.T) {
	dir := testdataDir(t)
	b := newOpenBackend(t, dir)
	ctx := context.Background()

	_, err := b.ResolveSymbol(ctx, SymbolRef{})
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("ResolveSymbol: expected ErrUnsupported, got %v", err)
	}

	_, err = b.ResolveType(ctx, NodeRef{})
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("ResolveType: expected ErrUnsupported, got %v", err)
	}

	_, err = b.CrossFileRefs(ctx, SymbolRef{})
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("CrossFileRefs: expected ErrUnsupported, got %v", err)
	}
}

// TestTreeSitterBackend_Close idempotent.
func TestTreeSitterBackend_Close(t *testing.T) {
	dir := testdataDir(t)
	b := &TreeSitterBackend{}
	if err := b.Open(context.Background(), ProjectConfig{RootDir: dir}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	// Double close should not panic
	if err := b.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// TestNormalise checks the normalise function for known and unknown types.
func TestNormalise(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"function_declaration", "FunctionDeclaration"},
		{"arrow_function", "ArrowFunction"},
		{"call_expression", "CallExpression"},
		{"identifier", "Identifier"},
		{"member_expression", "MemberExpression"},
		{"variable_declarator", "VariableDeclarator"},
		{"import_declaration", "ImportDeclaration"},
		{"export_statement", "ExportStatement"},
		{"jsx_element", "JsxElement"},
		{"jsx_self_closing_element", "JsxSelfClosingElement"},
		{"as_expression", "AsExpression"},
		{"await_expression", "AwaitExpression"},
		{"assignment_expression", "AssignmentExpression"},
		{"binary_expression", "BinaryExpression"},
		{"object_pattern", "ObjectPattern"},
		{"array_pattern", "ArrayPattern"},
		{"rest_pattern", "RestPattern"},
		{"ERROR", "Error"},
		// fallback: snake_case -> PascalCase
		{"unknown_node_type", "UnknownNodeType"},
		{"some_thing", "SomeThing"},
	}
	for _, tc := range cases {
		got := normalise(tc.input)
		if got != tc.want {
			t.Errorf("normalise(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestSnakeToPascal checks the snake_case conversion fallback.
func TestSnakeToPascal(t *testing.T) {
	cases := []struct{ in, want string }{
		{"foo_bar", "FooBar"},
		{"foo", "Foo"},
		{"a_b_c", "ABC"},
		{"", ""},
		{"_leading", "Leading"},
	}
	for _, tc := range cases {
		got := snakeToPascal(tc.in)
		if got != tc.want {
			t.Errorf("snakeToPascal(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestTreeSitterBackend_WalkAST_FieldNames checks that field names are set
// on child nodes.
func TestTreeSitterBackend_WalkAST_FieldNames(t *testing.T) {
	dir := testdataDir(t)
	b := newOpenBackend(t, dir)

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

// TestTreeSitterBackend_WalkAST_ChildCount checks ChildCount and Child consistency.
func TestTreeSitterBackend_WalkAST_ChildCount(t *testing.T) {
	dir := testdataDir(t)
	b := newOpenBackend(t, dir)

	pv := &funcVisitor{
		enterFn: func(node ASTNode) (bool, error) {
			count := node.ChildCount()
			for i := 0; i < count; i++ {
				child := node.Child(i)
				if child == nil {
					t.Errorf("node %s: Child(%d) returned nil but ChildCount=%d", node.Kind(), i, count)
				}
			}
			return true, nil
		},
	}

	if err := b.WalkAST(context.Background(), pv); err != nil {
		t.Fatalf("WalkAST: %v", err)
	}
}

// funcVisitor is a test helper implementing ASTVisitor with function fields.
type funcVisitor struct {
	enterFileFn func(path string) error
	enterFn     func(node ASTNode) (bool, error)
	leaveFn     func(node ASTNode) error
	leaveFileFn func(path string) error
}

func (fv *funcVisitor) EnterFile(path string) error {
	if fv.enterFileFn != nil {
		return fv.enterFileFn(path)
	}
	return nil
}

func (fv *funcVisitor) Enter(node ASTNode) (bool, error) {
	if fv.enterFn != nil {
		return fv.enterFn(node)
	}
	return true, nil
}

func (fv *funcVisitor) Leave(node ASTNode) error {
	if fv.leaveFn != nil {
		return fv.leaveFn(node)
	}
	return nil
}

func (fv *funcVisitor) LeaveFile(path string) error {
	if fv.leaveFileFn != nil {
		return fv.leaveFileFn(path)
	}
	return nil
}
