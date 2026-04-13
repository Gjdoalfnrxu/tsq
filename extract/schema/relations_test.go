package schema

import (
	"testing"
)

func TestAllRelationsRegistered(t *testing.T) {
	expected := []string{
		"File", "Node", "Contains",
		"Symbol", "FunctionSymbol",
		"Function", "Parameter", "ParameterRest", "ParameterOptional", "ParamIsFunctionType",
		"Call", "CallArg", "CallArgSpread", "CallCalleeSym", "CallResultSym",
		"VarDecl", "Assign",
		"ExprMayRef", "ExprIsCall", "FieldRead", "FieldWrite", "Await", "Cast",
		"DestructureField", "ArrayDestructure", "DestructureRest",
		"ImportBinding", "ExportBinding", "TypeFromLib",
		"JsxElement", "JsxAttribute",
		// v2 type-aware relations
		"ClassDecl", "InterfaceDecl", "Implements", "Extends",
		"MethodDecl", "MethodCall", "NewExpr", "ExprType",
		"TypeDecl", "ReturnStmt", "FunctionContains", "SymInFunction",
		// v3 type-fact relations
		"TypeInfo", "TypeMember", "UnionMember", "IntersectionMember",
		"GenericInstantiation", "TypeAlias", "TypeParameter",
		// C1: Template literals
		"TemplateLiteral", "TemplateElement", "TemplateExpression",
		// C2: Enum declarations
		"EnumDecl", "EnumMember",
		// C5: Optional chaining and nullish coalescing
		"OptionalChain", "NullishCoalescing",
		// C3: Decorator extraction
		"Decorator",
		// C4: Namespace/module declaration extraction
		"NamespaceDecl", "NamespaceMember",
		// C6: TypeScript type guards and assertion functions
		"TypeGuard",
		"ExtractError", "SchemaVersion",
	}
	for _, name := range expected {
		def, ok := Lookup(name)
		if !ok {
			t.Errorf("relation %q not found in registry", name)
			continue
		}
		if err := def.Validate(); err != nil {
			t.Errorf("relation %q fails validation: %v", name, err)
		}
	}
}

func TestRelationCount(t *testing.T) {
	if len(Registry) != 93 {
		t.Fatalf("expected 93 relations in registry, got %d", len(Registry))
	}
}

func TestV2RelationsRegistered(t *testing.T) {
	v2Relations := []string{
		"ClassDecl", "InterfaceDecl", "Implements", "Extends",
		"MethodDecl", "MethodCall", "NewExpr", "ExprType",
		"TypeDecl", "ReturnStmt", "FunctionContains", "SymInFunction",
	}
	for _, name := range v2Relations {
		def, ok := Lookup(name)
		if !ok {
			t.Errorf("v2 relation %q not found in registry", name)
			continue
		}
		if def.Version != 2 {
			t.Errorf("v2 relation %q: expected Version=2, got %d", name, def.Version)
		}
		if err := def.Validate(); err != nil {
			t.Errorf("v2 relation %q fails validation: %v", name, err)
		}
	}
}

func TestNodeRelationColumns(t *testing.T) {
	def, ok := Lookup("Node")
	if !ok {
		t.Fatal("Node not found")
	}
	if def.Arity() != 7 {
		t.Fatalf("expected 7 columns, got %d", def.Arity())
	}
	// Check column types
	expectedTypes := []ColumnType{
		TypeEntityRef, TypeEntityRef, TypeString,
		TypeInt32, TypeInt32, TypeInt32, TypeInt32,
	}
	for i, col := range def.Columns {
		if col.Type != expectedTypes[i] {
			t.Errorf("column %d (%q): expected type %d, got %d", i, col.Name, expectedTypes[i], col.Type)
		}
	}
}

func TestFileRelationColumns(t *testing.T) {
	def, ok := Lookup("File")
	if !ok {
		t.Fatal("File not found")
	}
	if def.Arity() != 3 {
		t.Fatalf("expected 3 columns, got %d", def.Arity())
	}
	expectedNames := []string{"id", "path", "contentHash"}
	for i, col := range def.Columns {
		if col.Name != expectedNames[i] {
			t.Errorf("column %d: expected name %q, got %q", i, expectedNames[i], col.Name)
		}
	}
}
