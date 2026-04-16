package rules

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// TestColPosOrdering verifies that colPos places terms at the positions
// dictated by the schema, not by the order they appear in the map.
// Assign columns: [lhsNode(0), rhsExpr(1), lhsSym(2)].
func TestColPosOrdering(t *testing.T) {
	args := colPos("Assign", cols{
		"lhsSym":  v("lhsSym"),
		"rhsExpr": v("rhsExpr"),
		// lhsNode omitted → should be Wildcard
	})

	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(args))
	}

	// col 0 = lhsNode → should be Wildcard (not specified)
	if _, ok := args[0].(datalog.Wildcard); !ok {
		t.Errorf("col 0 (lhsNode): expected Wildcard, got %T", args[0])
	}
	// col 1 = rhsExpr → should be Var{Name:"rhsExpr"}
	if vr, ok := args[1].(datalog.Var); !ok || vr.Name != "rhsExpr" {
		t.Errorf("col 1 (rhsExpr): expected Var{rhsExpr}, got %v", args[1])
	}
	// col 2 = lhsSym → should be Var{Name:"lhsSym"}
	if vr, ok := args[2].(datalog.Var); !ok || vr.Name != "lhsSym" {
		t.Errorf("col 2 (lhsSym): expected Var{lhsSym}, got %v", args[2])
	}
}

// TestColPosAllWildcard verifies that an empty namedCols map produces
// all-Wildcard args (length == arity of the relation).
func TestColPosAllWildcard(t *testing.T) {
	// CallArg has 3 columns: call, idx, argNode
	args := colPos("CallArg", cols{})
	if len(args) != 3 {
		t.Fatalf("expected 3 args for CallArg, got %d", len(args))
	}
	for i, arg := range args {
		if _, ok := arg.(datalog.Wildcard); !ok {
			t.Errorf("arg[%d]: expected Wildcard, got %T", i, arg)
		}
	}
}

// TestColPosTaintAlert verifies correct ordering for TaintAlert which has
// columns [srcExpr(0), sinkExpr(1), srcKind(2), sinkKind(3)].
func TestColPosTaintAlert(t *testing.T) {
	args := colPos("TaintAlert", cols{
		"sinkKind": v("sinkKind"),
		"srcExpr":  v("srcExpr"),
		"sinkExpr": v("sinkExpr"),
		"srcKind":  v("srcKind"),
	})

	if len(args) != 4 {
		t.Fatalf("expected 4 args for TaintAlert, got %d", len(args))
	}

	wantOrder := []string{"srcExpr", "sinkExpr", "srcKind", "sinkKind"}
	for i, name := range wantOrder {
		vr, ok := args[i].(datalog.Var)
		if !ok {
			t.Errorf("col %d (%s): expected Var, got %T", i, name, args[i])
			continue
		}
		if vr.Name != name {
			t.Errorf("col %d: expected Var{%s}, got Var{%s}", i, name, vr.Name)
		}
	}
}

// TestColPosUnknownRelation verifies that using an unregistered relation name panics.
func TestColPosUnknownRelation(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for unknown relation, got none")
		}
	}()
	colPos("NoSuchRelation_xyz", cols{})
}

// TestColPosUnknownColumn verifies that using an unknown column name panics.
func TestColPosUnknownColumn(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for unknown column, got none")
		}
	}()
	colPos("Assign", cols{"doesNotExist": v("x")})
}

// TestPosLitBuildsCorrectLiteral verifies posLit returns a positive literal
// with the correct predicate name and positional args.
func TestPosLitBuildsCorrectLiteral(t *testing.T) {
	lit := posLit("Assign", cols{
		"lhsSym":  v("lhsSym"),
		"rhsExpr": v("rhsExpr"),
	})

	if !lit.Positive {
		t.Error("expected positive literal")
	}
	if lit.Atom.Predicate != "Assign" {
		t.Errorf("expected predicate Assign, got %s", lit.Atom.Predicate)
	}
	if len(lit.Atom.Args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(lit.Atom.Args))
	}
}

// TestNegLitBuildsCorrectLiteral verifies negLit returns a negative literal.
func TestNegLitBuildsCorrectLiteral(t *testing.T) {
	lit := negLit("Assign", cols{
		"lhsSym": v("lhsSym"),
	})

	if lit.Positive {
		t.Error("expected negative literal")
	}
	if lit.Atom.Predicate != "Assign" {
		t.Errorf("expected predicate Assign, got %s", lit.Atom.Predicate)
	}
}

// TestColPosLocalFlow verifies LocalFlow positional ordering.
// LocalFlow columns: [fnId(0), srcSym(1), dstSym(2)].
func TestColPosLocalFlow(t *testing.T) {
	args := colPos("LocalFlow", cols{
		"dstSym": v("dst"),
		"srcSym": v("src"),
		"fnId":   v("fn"),
	})

	if len(args) != 3 {
		t.Fatalf("expected 3 args for LocalFlow, got %d", len(args))
	}

	type check struct {
		pos  int
		name string
	}
	for _, c := range []check{{0, "fn"}, {1, "src"}, {2, "dst"}} {
		vr, ok := args[c.pos].(datalog.Var)
		if !ok {
			t.Errorf("col %d: expected Var, got %T", c.pos, args[c.pos])
			continue
		}
		if vr.Name != c.name {
			t.Errorf("col %d: expected Var{%s}, got Var{%s}", c.pos, c.name, vr.Name)
		}
	}
}
