package eval

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// makeRelation is a helper that builds a Relation from raw int/string slices.
// vals is a flat list: [col0row0, col1row0, col0row1, col1row1, ...]
func makeRelation(name string, arity int, vals ...Value) *Relation {
	r := NewRelation(name, arity)
	for i := 0; i+arity <= len(vals); i += arity {
		t := make(Tuple, arity)
		copy(t, vals[i:i+arity])
		r.Add(t)
	}
	return r
}

// positiveStep builds a positive JoinStep from an atom.
func positiveStep(pred string, args ...datalog.Term) plan.JoinStep {
	return plan.JoinStep{
		Literal: datalog.Literal{
			Positive: true,
			Atom:     datalog.Atom{Predicate: pred, Args: args},
		},
	}
}

// negativeStep builds a negative JoinStep from an atom.
func negativeStep(pred string, args ...datalog.Term) plan.JoinStep {
	return plan.JoinStep{
		Literal: datalog.Literal{
			Positive: false,
			Atom:     datalog.Atom{Predicate: pred, Args: args},
		},
		IsFilter: true,
	}
}

// cmpStep builds a comparison JoinStep.
func cmpStep(op string, left, right datalog.Term) plan.JoinStep {
	return plan.JoinStep{
		Literal: datalog.Literal{
			Positive: true,
			Cmp:      &datalog.Comparison{Op: op, Left: left, Right: right},
		},
		IsFilter: true,
	}
}

func v(name string) datalog.Var   { return datalog.Var{Name: name} }
func ic(n int64) datalog.IntConst { return datalog.IntConst{Value: n} }

// head builds a PlannedRule head atom.
func head(pred string, args ...datalog.Term) datalog.Atom {
	return datalog.Atom{Predicate: pred, Args: args}
}

// Test 2-relation join: Edge(x,y) ∧ Edge(y,z) → Path(x,z)
// Edge: (1,2),(2,3),(3,4)
// 2-hop paths via common intermediate:
//
//	x=1,y=2 → Edge(2,z): z=3 → Path(1,3)
//	x=2,y=3 → Edge(3,z): z=4 → Path(2,4)
//	x=3,y=4 → no Edge(4,...) → nothing
//
// Result: 2 tuples.
func TestEvalRuleTwoRelationJoin(t *testing.T) {
	edge := makeRelation("Edge", 2,
		IntVal{1}, IntVal{2},
		IntVal{2}, IntVal{3},
		IntVal{3}, IntVal{4},
	)
	rels := RelsOf(edge)

	rule := plan.PlannedRule{
		Head: head("Path", v("x"), v("z")),
		JoinOrder: []plan.JoinStep{
			positiveStep("Edge", v("x"), v("y")),
			positiveStep("Edge", v("y"), v("z")),
		},
	}

	results, err := Rule(context.Background(), rule, rels, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 path tuples (2-hops), got %d: %v", len(results), results)
	}
	expected := map[string]bool{
		tupleKey(Tuple{IntVal{1}, IntVal{3}}): true,
		tupleKey(Tuple{IntVal{2}, IntVal{4}}): true,
	}
	for _, r := range results {
		if !expected[tupleKey(r)] {
			t.Errorf("unexpected tuple: %v", r)
		}
	}
}

// Test 3-relation join: R(x,y) ∧ S(y,z) ∧ T(z,w) → Q(x,w)
func TestEvalRuleThreeRelationJoin(t *testing.T) {
	R := makeRelation("R", 2, IntVal{1}, IntVal{2})
	S := makeRelation("S", 2, IntVal{2}, IntVal{3})
	T := makeRelation("T", 2, IntVal{3}, IntVal{4})
	rels := RelsOf(R, S, T)

	rule := plan.PlannedRule{
		Head: head("Q", v("x"), v("w")),
		JoinOrder: []plan.JoinStep{
			positiveStep("R", v("x"), v("y")),
			positiveStep("S", v("y"), v("z")),
			positiveStep("T", v("z"), v("w")),
		},
	}

	results, err := Rule(context.Background(), rule, rels, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	got := results[0]
	if got[0] != (IntVal{1}) || got[1] != (IntVal{4}) {
		t.Errorf("expected (1,4), got %v", got)
	}
}

// Test no-match join: no common key between A and B.
func TestEvalRuleNoMatch(t *testing.T) {
	A := makeRelation("A", 2, IntVal{1}, IntVal{2})
	B := makeRelation("B", 2, IntVal{99}, IntVal{100})
	rels := RelsOf(A, B)

	rule := plan.PlannedRule{
		Head: head("Q", v("x"), v("z")),
		JoinOrder: []plan.JoinStep{
			positiveStep("A", v("x"), v("y")),
			positiveStep("B", v("y"), v("z")), // y from A won't match B's first col
		},
	}

	results, err := Rule(context.Background(), rule, rels, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d: %v", len(results), results)
	}
}

// Test comparison filter: R(x,y) ∧ x < 3 → Q(x,y)
func TestEvalRuleComparisonFilter(t *testing.T) {
	R := makeRelation("R", 2,
		IntVal{1}, IntVal{10},
		IntVal{2}, IntVal{20},
		IntVal{3}, IntVal{30},
		IntVal{4}, IntVal{40},
	)
	rels := RelsOf(R)

	rule := plan.PlannedRule{
		Head: head("Q", v("x"), v("y")),
		JoinOrder: []plan.JoinStep{
			positiveStep("R", v("x"), v("y")),
			cmpStep("<", v("x"), ic(3)),
		},
	}

	results, err := Rule(context.Background(), rule, rels, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (x=1,x=2), got %d: %v", len(results), results)
	}
}

// Test self-join: Edge(x,y) ∧ Edge(x,y) → Q(x,y)
func TestEvalRuleSelfJoin(t *testing.T) {
	edge := makeRelation("Edge", 2,
		IntVal{1}, IntVal{2},
		IntVal{2}, IntVal{3},
	)
	rels := RelsOf(edge)

	rule := plan.PlannedRule{
		Head: head("Q", v("x"), v("y")),
		JoinOrder: []plan.JoinStep{
			positiveStep("Edge", v("x"), v("y")),
			positiveStep("Edge", v("x"), v("y")), // same binding constraint
		},
	}

	results, err := Rule(context.Background(), rule, rels, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Each edge should match exactly itself once.
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %v", len(results), results)
	}
}

// Test anti-join (negative literal): A(x) ∧ not B(x) → Q(x)
func TestEvalRuleAntiJoin(t *testing.T) {
	A := makeRelation("A", 1, IntVal{1}, IntVal{2}, IntVal{3})
	B := makeRelation("B", 1, IntVal{2})
	rels := RelsOf(A, B)

	rule := plan.PlannedRule{
		Head: head("Q", v("x")),
		JoinOrder: []plan.JoinStep{
			positiveStep("A", v("x")),
			negativeStep("B", v("x")),
		},
	}

	results, err := Rule(context.Background(), rule, rels, 0)
	if err != nil {
		t.Fatal(err)
	}
	// x=2 is in B, so excluded. Expected: x=1, x=3.
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %v", len(results), results)
	}
	seen := map[int64]bool{}
	for _, r := range results {
		seen[r[0].(IntVal).V] = true
	}
	if !seen[1] || !seen[3] {
		t.Errorf("expected x=1 and x=3 in results, got %v", results)
	}
	if seen[2] {
		t.Error("x=2 should be excluded by anti-join")
	}
}

// TestRuleBindingCapTriggers proves the per-rule cardinality cap (issue #80)
// fires before the unbounded intermediate-binding slice can OOM the process.
//
// We construct a 4-way Cartesian-style join over four small unary base
// relations:
//
//	BadRule(a, b, c, d) :- A(a), B(b), C(c), D(d).
//
// Each base relation has 10 tuples and shares no variables with the others,
// so the intermediate cardinality grows multiplicatively: 10 → 100 → 1000 →
// 10000 by the final step. With a cap of 100, evaluation must error after
// the second join (when cardinality first exceeds 100), well before reaching
// the 10000-row final result.
//
// The test asserts:
//  1. Rule returns a non-nil error.
//  2. The error wraps ErrBindingCapExceeded (so callers can detect it).
//  3. The wrapped *BindingCapError carries the right rule name and cap.
func TestRuleBindingCapTriggers(t *testing.T) {
	// Four 10-tuple unary relations with no shared columns.
	mkUnary := func(name string) *Relation {
		vals := make([]Value, 10)
		for i := 0; i < 10; i++ {
			vals[i] = IntVal{V: int64(i)}
		}
		return makeRelation(name, 1, vals...)
	}
	rels := RelsOf(mkUnary("A"), mkUnary("B"), mkUnary("C"), mkUnary("D"))

	rule := plan.PlannedRule{
		Head: head("BadRule", v("a"), v("b"), v("c"), v("d")),
		JoinOrder: []plan.JoinStep{
			positiveStep("A", v("a")),
			positiveStep("B", v("b")),
			positiveStep("C", v("c")),
			positiveStep("D", v("d")),
		},
	}

	const cap = 100
	results, err := Rule(context.Background(), rule, rels, cap)
	if err == nil {
		t.Fatalf("expected ErrBindingCapExceeded, got nil error and %d results", len(results))
	}
	if !errors.Is(err, ErrBindingCapExceeded) {
		t.Fatalf("expected error wrapping ErrBindingCapExceeded, got: %v", err)
	}
	var bce *BindingCapError
	if !errors.As(err, &bce) {
		t.Fatalf("expected *BindingCapError, got: %T (%v)", err, err)
	}
	if bce.Rule != "BadRule" {
		t.Errorf("expected rule name %q in error, got %q", "BadRule", bce.Rule)
	}
	if bce.Cap != cap {
		t.Errorf("expected cap %d in error, got %d", cap, bce.Cap)
	}
	if bce.Cardinality <= cap {
		t.Errorf("expected reported cardinality > cap (%d), got %d", cap, bce.Cardinality)
	}
	// Promptness: cap must fire at the boundary, not after the join has already
	// blown past it by orders of magnitude. Step 2 (A×B) yields 100 = cap; step 3
	// (×C) yields 1000 — that's the latest the cap may legitimately fire.
	if bce.Cardinality > 2*cap {
		t.Errorf("cap fired late: cardinality=%d, cap=%d (expected <= 2*cap)", bce.Cardinality, cap)
	}
	// Must trip before the final join step completes, otherwise the inner-loop
	// check has been silently lost and only the per-step check is firing.
	if bce.StepIndex >= len(rule.JoinOrder)-1 {
		t.Errorf("cap fired at step %d (last step); expected earlier step (inner-loop check missing?)", bce.StepIndex)
	}
	if !strings.Contains(err.Error(), "BadRule") || !strings.Contains(err.Error(), "binding cap") {
		t.Errorf("error message should mention rule name and binding cap: %q", err.Error())
	}
}

// TestRuleBindingCapDisabled verifies that passing 0 (or negative) disables
// the cap entirely — the same query that errors above must succeed when the
// cap is off, returning the full Cartesian product.
func TestRuleBindingCapDisabled(t *testing.T) {
	mkUnary := func(name string) *Relation {
		vals := make([]Value, 5)
		for i := 0; i < 5; i++ {
			vals[i] = IntVal{V: int64(i)}
		}
		return makeRelation(name, 1, vals...)
	}
	rels := RelsOf(mkUnary("A"), mkUnary("B"))

	rule := plan.PlannedRule{
		Head: head("Cross", v("a"), v("b")),
		JoinOrder: []plan.JoinStep{
			positiveStep("A", v("a")),
			positiveStep("B", v("b")),
		},
	}

	results, err := Rule(context.Background(), rule, rels, 0)
	if err != nil {
		t.Fatalf("unexpected error with cap disabled: %v", err)
	}
	if len(results) != 25 {
		t.Errorf("expected full cross product of 25 tuples, got %d", len(results))
	}
}
