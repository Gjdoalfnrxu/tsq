package plan_test

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// cmpLit creates a comparison literal.
func cmpLit(op string, left, right string) datalog.Literal {
	return datalog.Literal{
		Positive: true,
		Cmp: &datalog.Comparison{
			Op:    op,
			Left:  datalog.Var{Name: left},
			Right: datalog.Var{Name: right},
		},
	}
}

// aggLit creates an aggregate literal with a result variable.
func aggLit(fn, bodyPred, bodyVar, resultVar string) datalog.Literal {
	return datalog.Literal{
		Positive: true,
		Agg: &datalog.Aggregate{
			Func:      fn,
			Var:       bodyVar,
			TypeName:  "T",
			Body:      []datalog.Literal{posLit(bodyPred, bodyVar)},
			ResultVar: datalog.Var{Name: resultVar},
		},
	}
}

// TestJoinSingleLiteral: one literal → one step.
func TestJoinSingleLiteral(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("P", "x"), posLit("A", "x")),
		},
	}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(ep.Strata) == 0 || len(ep.Strata[0].Rules) == 0 {
		t.Fatal("no strata or rules")
	}
	r := ep.Strata[0].Rules[0]
	if len(r.JoinOrder) != 1 {
		t.Errorf("expected 1 join step, got %d", len(r.JoinOrder))
	}
	if r.JoinOrder[0].Literal.Atom.Predicate != "A" {
		t.Errorf("expected step for A, got %s", r.JoinOrder[0].Literal.Atom.Predicate)
	}
}

// TestJoinTwoLiteralsSharedVar: shared variable guides join order.
func TestJoinTwoLiteralsSharedVar(t *testing.T) {
	// P(x, y) :- A(x), B(x, y).
	// A and B both eligible first; no size hints → default 1000 each → A placed first (first eligible),
	// then B (x is now bound, one var bound).
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: atom("P", "x", "y"),
				Body: []datalog.Literal{posLit("A", "x"), posLit("B", "x", "y")},
			},
		},
	}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	r := ep.Strata[0].Rules[0]
	if len(r.JoinOrder) != 2 {
		t.Errorf("expected 2 join steps, got %d", len(r.JoinOrder))
	}
}

// TestJoinThreeLiteralsGreedy: most-bound-first selection.
func TestJoinThreeLiteralsGreedy(t *testing.T) {
	// P(x, y, z) :- A(x), B(x, y), C(y, z).
	// Start: A eligible (0 bound vars), B eligible (0), C eligible (0).
	// Ties broken by size. All same size → A placed first (stable index).
	// After A: x bound. B has 1 bound var (x), C has 0.
	// B placed. After B: x, y bound. C has 1 bound var (y). C placed.
	hints := map[string]int{"A": 10, "B": 100, "C": 50}
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: atom("P", "x", "y", "z"),
				Body: []datalog.Literal{posLit("A", "x"), posLit("B", "x", "y"), posLit("C", "y", "z")},
			},
		},
	}
	ep, errs := plan.Plan(prog, hints)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	r := ep.Strata[0].Rules[0]
	if len(r.JoinOrder) != 3 {
		t.Fatalf("expected 3 join steps, got %d", len(r.JoinOrder))
	}
	// A should be first (smallest size among tie of 0-bound-var relations).
	if r.JoinOrder[0].Literal.Atom.Predicate != "A" {
		t.Errorf("expected A first (smallest size), got %s", r.JoinOrder[0].Literal.Atom.Predicate)
	}
	// B second.
	if r.JoinOrder[1].Literal.Atom.Predicate != "B" {
		t.Errorf("expected B second, got %s", r.JoinOrder[1].Literal.Atom.Predicate)
	}
	// C last.
	if r.JoinOrder[2].Literal.Atom.Predicate != "C" {
		t.Errorf("expected C last, got %s", r.JoinOrder[2].Literal.Atom.Predicate)
	}
}

// TestJoinSizeHintsTieBreaking: smaller relation placed first when bound counts tie.
func TestJoinSizeHintsTieBreaking(t *testing.T) {
	// P(x) :- BigRel(x), SmallRel(x).
	// Both have x unbound initially; SmallRel has smaller hint → placed first.
	hints := map[string]int{"BigRel": 9000, "SmallRel": 5}
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: atom("P", "x"),
				Body: []datalog.Literal{posLit("BigRel", "x"), posLit("SmallRel", "x")},
			},
		},
	}
	ep, errs := plan.Plan(prog, hints)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	r := ep.Strata[0].Rules[0]
	if len(r.JoinOrder) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(r.JoinOrder))
	}
	if r.JoinOrder[0].Literal.Atom.Predicate != "SmallRel" {
		t.Errorf("expected SmallRel first (smaller size), got %s", r.JoinOrder[0].Literal.Atom.Predicate)
	}
}

// TestJoinComparisonPlacedAfterOperandsBound.
func TestJoinComparisonPlacedAfterOperandsBound(t *testing.T) {
	// P(x, y) :- A(x), B(y), x < y.
	// Comparison x < y only eligible after both x and y are bound.
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: atom("P", "x", "y"),
				Body: []datalog.Literal{posLit("A", "x"), posLit("B", "y"), cmpLit("<", "x", "y")},
			},
		},
	}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	r := ep.Strata[0].Rules[0]
	if len(r.JoinOrder) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(r.JoinOrder))
	}
	last := r.JoinOrder[2]
	if last.Literal.Cmp == nil {
		t.Errorf("expected comparison as last step, got atom %s", last.Literal.Atom.Predicate)
	}
}

// TestJoinNegativeLiteralPlacedAfterVarsBound.
func TestJoinNegativeLiteralPlacedAfterVarsBound(t *testing.T) {
	// P(x) :- A(x), not B(x).
	// not B(x) only eligible after x is bound.
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: atom("P", "x"),
				Body: []datalog.Literal{posLit("A", "x"), negLit("B", "x")},
			},
		},
	}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	r := ep.Strata[0].Rules[0]
	if len(r.JoinOrder) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(r.JoinOrder))
	}
	if r.JoinOrder[0].Literal.Atom.Predicate != "A" {
		t.Errorf("expected A first, got %s", r.JoinOrder[0].Literal.Atom.Predicate)
	}
	if r.JoinOrder[1].Literal.Positive {
		t.Errorf("expected last step to be negative literal")
	}
}
