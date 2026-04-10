package extract

import (
	"testing"
)

// TestNormaliseTsgoKind checks kind normalisation for known tsgo kinds.
func TestNormaliseTsgoKind(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"FunctionDeclaration", "FunctionDeclaration"},
		{"ArrowFunction", "ArrowFunction"},
		{"CallExpression", "CallExpression"},
		{"Identifier", "Identifier"},
		{"PropertyAccessExpression", "MemberExpression"},
		{"VariableDeclaration", "VariableDeclarator"},
		{"ImportDeclaration", "ImportDeclaration"},
		{"ExportDeclaration", "ExportStatement"},
		{"JsxElement", "JsxElement"},
		{"JsxSelfClosingElement", "JsxSelfClosingElement"},
		{"AsExpression", "AsExpression"},
		{"AwaitExpression", "AwaitExpression"},
		{"BinaryExpression", "BinaryExpression"},
		{"ObjectBindingPattern", "ObjectPattern"},
		{"ArrayBindingPattern", "ArrayPattern"},
		{"SourceFile", "Program"},
		{"Block", "Block"},
		{"StringLiteral", "String"},
		{"NumericLiteral", "Number"},
		{"InterfaceDeclaration", "InterfaceDeclaration"},
		{"TypeAliasDeclaration", "TypeAliasDeclaration"},
		{"MethodDeclaration", "MethodDefinition"},
		{"ClassDeclaration", "ClassDeclaration"},
		// Unknown kinds: already PascalCase pass through
		{"SomeUnknownKind", "SomeUnknownKind"},
		// Lowercase unknown: converted to PascalCase
		{"some_unknown_kind", "SomeUnknownKind"},
	}
	for _, tc := range cases {
		got := normaliseTsgoKind(tc.input)
		if got != tc.want {
			t.Errorf("normaliseTsgoKind(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestTsgoSnakeToPascal checks the fallback snake_case conversion.
func TestTsgoSnakeToPascal(t *testing.T) {
	cases := []struct{ in, want string }{
		{"foo_bar", "FooBar"},
		{"a_b_c", "ABC"},
		{"single", "Single"},
		{"", ""},
		{"_leading", "Leading"},
	}
	for _, tc := range cases {
		got := tsgoSnakeToPascal(tc.in)
		if got != tc.want {
			t.Errorf("tsgoSnakeToPascal(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestTsgoNode_Kind checks that Kind() normalises correctly.
func TestTsgoNode_Kind(t *testing.T) {
	n := newTsgoNode("PropertyAccessExpression", 1, 0, 1, 10, "obj.prop")
	if got := n.Kind(); got != "MemberExpression" {
		t.Errorf("Kind() = %q, want %q", got, "MemberExpression")
	}
}

// TestTsgoNode_Position checks position accessors.
func TestTsgoNode_Position(t *testing.T) {
	n := newTsgoNode("Identifier", 5, 10, 5, 15, "myVar")

	if n.StartLine() != 5 {
		t.Errorf("StartLine() = %d, want 5", n.StartLine())
	}
	if n.StartCol() != 10 {
		t.Errorf("StartCol() = %d, want 10", n.StartCol())
	}
	if n.EndLine() != 5 {
		t.Errorf("EndLine() = %d, want 5", n.EndLine())
	}
	if n.EndCol() != 15 {
		t.Errorf("EndCol() = %d, want 15", n.EndCol())
	}
}

// TestTsgoNode_Text checks Text() accessor.
func TestTsgoNode_Text(t *testing.T) {
	n := newTsgoNode("Identifier", 1, 0, 1, 3, "foo")
	if got := n.Text(); got != "foo" {
		t.Errorf("Text() = %q, want %q", got, "foo")
	}
}

// TestTsgoNode_Children checks child iteration.
func TestTsgoNode_Children(t *testing.T) {
	parent := newTsgoNode("CallExpression", 1, 0, 1, 20, "foo(bar)")
	child1 := newTsgoNode("Identifier", 1, 0, 1, 3, "foo")
	child1.setFieldName("function")
	child2 := newTsgoNode("Identifier", 1, 4, 1, 7, "bar")
	child2.setFieldName("arguments")
	parent.addChild(child1)
	parent.addChild(child2)

	if parent.ChildCount() != 2 {
		t.Errorf("ChildCount() = %d, want 2", parent.ChildCount())
	}

	c0 := parent.Child(0)
	if c0 == nil {
		t.Fatal("Child(0) returned nil")
	}
	if c0.Kind() != "Identifier" {
		t.Errorf("Child(0).Kind() = %q, want Identifier", c0.Kind())
	}
	if c0.FieldName() != "function" {
		t.Errorf("Child(0).FieldName() = %q, want function", c0.FieldName())
	}

	c1 := parent.Child(1)
	if c1 == nil {
		t.Fatal("Child(1) returned nil")
	}
	if c1.FieldName() != "arguments" {
		t.Errorf("Child(1).FieldName() = %q, want arguments", c1.FieldName())
	}
}

// TestTsgoNode_Child_OutOfBounds checks out-of-bounds returns nil.
func TestTsgoNode_Child_OutOfBounds(t *testing.T) {
	n := newTsgoNode("Identifier", 1, 0, 1, 3, "x")

	if got := n.Child(-1); got != nil {
		t.Error("Child(-1) should return nil")
	}
	if got := n.Child(0); got != nil {
		t.Error("Child(0) should return nil for childless node")
	}
	if got := n.Child(100); got != nil {
		t.Error("Child(100) should return nil")
	}
}

// TestTsgoNode_FieldName_Default checks that field name defaults to empty.
func TestTsgoNode_FieldName_Default(t *testing.T) {
	n := newTsgoNode("Identifier", 1, 0, 1, 3, "x")
	if got := n.FieldName(); got != "" {
		t.Errorf("FieldName() = %q, want empty", got)
	}
}

// TestTsgoNode_ImplementsASTNode verifies the ASTNode interface at compile time.
func TestTsgoNode_ImplementsASTNode(t *testing.T) {
	var _ ASTNode = (*tsgoNode)(nil)
}

// TestNewTsgoNode checks the constructor.
func TestNewTsgoNode(t *testing.T) {
	n := newTsgoNode("FunctionDeclaration", 1, 0, 5, 1, "function foo() {}")
	if n.kind != "FunctionDeclaration" {
		t.Errorf("kind = %q, want FunctionDeclaration", n.kind)
	}
	if n.startLine != 1 || n.startCol != 0 || n.endLine != 5 || n.endCol != 1 {
		t.Error("position fields not set correctly")
	}
	if n.text != "function foo() {}" {
		t.Errorf("text = %q, want 'function foo() {}'", n.text)
	}
	if len(n.children) != 0 {
		t.Errorf("expected 0 children, got %d", len(n.children))
	}
}
