package extract

import "testing"

func TestIsFunctionKind(t *testing.T) {
	// All entries in FunctionKinds should return true
	for _, k := range FunctionKinds {
		if !IsFunctionKind(k) {
			t.Errorf("IsFunctionKind(%q) = false, want true", k)
		}
	}

	// Non-function kinds should return false
	nonFunction := []string{
		"ClassDeclaration",
		"VariableDeclarator",
		"CallExpression",
		"Identifier",
		"",
	}
	for _, k := range nonFunction {
		if IsFunctionKind(k) {
			t.Errorf("IsFunctionKind(%q) = true, want false", k)
		}
	}
}

func TestFunctionKindsCompleteness(t *testing.T) {
	// Verify the known set of function kinds
	expected := map[string]bool{
		"FunctionDeclaration":          true,
		"ArrowFunction":                true,
		"FunctionExpression":           true,
		"MethodDefinition":             true,
		"GeneratorFunction":            true,
		"GeneratorFunctionDeclaration": true,
	}
	if len(FunctionKinds) != len(expected) {
		t.Errorf("FunctionKinds has %d entries, expected %d", len(FunctionKinds), len(expected))
	}
	for _, k := range FunctionKinds {
		if !expected[k] {
			t.Errorf("unexpected function kind: %q", k)
		}
	}
}
