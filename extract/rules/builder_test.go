package rules

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// TestNamedLiteral_CorrectOrdering verifies that NamedLiteral places terms in
// the column order defined by the schema registry, not the order they appear in
// the cols map. This is the core correctness guarantee of the builder.
func TestNamedLiteral_CorrectOrdering(t *testing.T) {
	// Assign schema: {lhsNode, rhsExpr, lhsSym} (positions 0, 1, 2)
	// Pass cols in reverse order to confirm positional placement is schema-driven.
	lit, err := NamedLiteral("Assign", map[string]datalog.Term{
		"lhsSym":  v("lhsSymVar"),
		"rhsExpr": v("rhsExprVar"),
		"lhsNode": v("lhsNodeVar"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !lit.Positive {
		t.Error("expected Positive = true")
	}
	if lit.Atom.Predicate != "Assign" {
		t.Errorf("expected predicate Assign, got %q", lit.Atom.Predicate)
	}
	if len(lit.Atom.Args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(lit.Atom.Args))
	}

	// Column 0 = lhsNode
	arg0, ok := lit.Atom.Args[0].(datalog.Var)
	if !ok {
		t.Errorf("col 0 (lhsNode): expected Var, got %T", lit.Atom.Args[0])
	} else if arg0.Name != "lhsNodeVar" {
		t.Errorf("col 0 (lhsNode): expected var name %q, got %q", "lhsNodeVar", arg0.Name)
	}

	// Column 1 = rhsExpr
	arg1, ok := lit.Atom.Args[1].(datalog.Var)
	if !ok {
		t.Errorf("col 1 (rhsExpr): expected Var, got %T", lit.Atom.Args[1])
	} else if arg1.Name != "rhsExprVar" {
		t.Errorf("col 1 (rhsExpr): expected var name %q, got %q", "rhsExprVar", arg1.Name)
	}

	// Column 2 = lhsSym
	arg2, ok := lit.Atom.Args[2].(datalog.Var)
	if !ok {
		t.Errorf("col 2 (lhsSym): expected Var, got %T", lit.Atom.Args[2])
	} else if arg2.Name != "lhsSymVar" {
		t.Errorf("col 2 (lhsSym): expected var name %q, got %q", "lhsSymVar", arg2.Name)
	}
}

// TestNamedLiteral_WildcardForMissingCols verifies that columns absent from
// the cols map are substituted with datalog.Wildcard{}.
func TestNamedLiteral_WildcardForMissingCols(t *testing.T) {
	// Assign: {lhsNode(0), rhsExpr(1), lhsSym(2)}
	// Only provide rhsExpr and lhsSym — lhsNode should become Wildcard.
	lit, err := NamedLiteral("Assign", map[string]datalog.Term{
		"rhsExpr": v("rhs"),
		"lhsSym":  v("lhs"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(lit.Atom.Args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(lit.Atom.Args))
	}

	// Col 0 (lhsNode) should be Wildcard.
	if _, ok := lit.Atom.Args[0].(datalog.Wildcard); !ok {
		t.Errorf("col 0 (lhsNode): expected Wildcard, got %T", lit.Atom.Args[0])
	}

	// Col 1 (rhsExpr) should be Var "rhs".
	if arg, ok := lit.Atom.Args[1].(datalog.Var); !ok || arg.Name != "rhs" {
		t.Errorf("col 1 (rhsExpr): expected Var{rhs}, got %T %v", lit.Atom.Args[1], lit.Atom.Args[1])
	}

	// Col 2 (lhsSym) should be Var "lhs".
	if arg, ok := lit.Atom.Args[2].(datalog.Var); !ok || arg.Name != "lhs" {
		t.Errorf("col 2 (lhsSym): expected Var{lhs}, got %T %v", lit.Atom.Args[2], lit.Atom.Args[2])
	}
}

// TestNamedLiteral_CallArgOrdering verifies correct column ordering for CallArg,
// which is the most-migrated relation (used in composition, summaries, higherorder,
// frameworks). CallArg schema: {call(0), idx(1), argNode(2)}.
func TestNamedLiteral_CallArgOrdering(t *testing.T) {
	lit, err := NamedLiteral("CallArg", map[string]datalog.Term{
		"argNode": v("theArg"),
		"call":    v("theCall"),
		"idx":     datalog.IntConst{Value: 0},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(lit.Atom.Args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(lit.Atom.Args))
	}

	// Col 0 = call
	if arg, ok := lit.Atom.Args[0].(datalog.Var); !ok || arg.Name != "theCall" {
		t.Errorf("col 0 (call): expected Var{theCall}, got %T %v", lit.Atom.Args[0], lit.Atom.Args[0])
	}

	// Col 1 = idx (IntConst{0})
	if ic, ok := lit.Atom.Args[1].(datalog.IntConst); !ok || ic.Value != 0 {
		t.Errorf("col 1 (idx): expected IntConst{0}, got %T %v", lit.Atom.Args[1], lit.Atom.Args[1])
	}

	// Col 2 = argNode
	if arg, ok := lit.Atom.Args[2].(datalog.Var); !ok || arg.Name != "theArg" {
		t.Errorf("col 2 (argNode): expected Var{theArg}, got %T %v", lit.Atom.Args[2], lit.Atom.Args[2])
	}
}

// TestNamedLiteral_LocalFlowOrdering verifies correct ordering for LocalFlow:
// {fnId(0), srcSym(1), dstSym(2)}.
func TestNamedLiteral_LocalFlowOrdering(t *testing.T) {
	lit, err := NamedLiteral("LocalFlow", map[string]datalog.Term{
		"dstSym": v("dst"),
		"fnId":   v("fn"),
		"srcSym": v("src"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(lit.Atom.Args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(lit.Atom.Args))
	}

	checkVar := func(pos int, colName, wantName string) {
		t.Helper()
		arg, ok := lit.Atom.Args[pos].(datalog.Var)
		if !ok {
			t.Errorf("col %d (%s): expected Var, got %T", pos, colName, lit.Atom.Args[pos])
			return
		}
		if arg.Name != wantName {
			t.Errorf("col %d (%s): expected %q, got %q", pos, colName, wantName, arg.Name)
		}
	}

	checkVar(0, "fnId", "fn")
	checkVar(1, "srcSym", "src")
	checkVar(2, "dstSym", "dst")
}

// TestNamedLiteral_UnknownRelation verifies that NamedLiteral returns an error
// for an unregistered relation name.
func TestNamedLiteral_UnknownRelation(t *testing.T) {
	_, err := NamedLiteral("NonExistentRelation", map[string]datalog.Term{
		"col": v("x"),
	})
	if err == nil {
		t.Fatal("expected error for unknown relation, got nil")
	}
}

// TestNamedLiteral_UnknownColumnName verifies that NamedLiteral returns an error
// when cols contains a key that is not a valid column name for the relation.
func TestNamedLiteral_UnknownColumnName(t *testing.T) {
	_, err := NamedLiteral("Assign", map[string]datalog.Term{
		"notAColumn": v("x"), // Assign has no "notAColumn" column
	})
	if err == nil {
		t.Fatal("expected error for unknown column name, got nil")
	}
}

// TestMustNamedLiteral_PanicsOnBadRelation verifies that mustNamedLiteral panics
// when given an unregistered relation, ensuring startup-time failures are caught.
func TestMustNamedLiteral_PanicsOnBadRelation(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for unknown relation, but did not panic")
		}
	}()
	mustNamedLiteral("NoSuchRelation", map[string]datalog.Term{"x": v("y")})
}
