package eval

import (
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
	rels := map[string]*Relation{"Edge": edge}

	rule := plan.PlannedRule{
		Head: head("Path", v("x"), v("z")),
		JoinOrder: []plan.JoinStep{
			positiveStep("Edge", v("x"), v("y")),
			positiveStep("Edge", v("y"), v("z")),
		},
	}

	results := Rule(rule, rels)
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
	rels := map[string]*Relation{"R": R, "S": S, "T": T}

	rule := plan.PlannedRule{
		Head: head("Q", v("x"), v("w")),
		JoinOrder: []plan.JoinStep{
			positiveStep("R", v("x"), v("y")),
			positiveStep("S", v("y"), v("z")),
			positiveStep("T", v("z"), v("w")),
		},
	}

	results := Rule(rule, rels)
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
	rels := map[string]*Relation{"A": A, "B": B}

	rule := plan.PlannedRule{
		Head: head("Q", v("x"), v("z")),
		JoinOrder: []plan.JoinStep{
			positiveStep("A", v("x"), v("y")),
			positiveStep("B", v("y"), v("z")), // y from A won't match B's first col
		},
	}

	results := Rule(rule, rels)
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
	rels := map[string]*Relation{"R": R}

	rule := plan.PlannedRule{
		Head: head("Q", v("x"), v("y")),
		JoinOrder: []plan.JoinStep{
			positiveStep("R", v("x"), v("y")),
			cmpStep("<", v("x"), ic(3)),
		},
	}

	results := Rule(rule, rels)
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
	rels := map[string]*Relation{"Edge": edge}

	rule := plan.PlannedRule{
		Head: head("Q", v("x"), v("y")),
		JoinOrder: []plan.JoinStep{
			positiveStep("Edge", v("x"), v("y")),
			positiveStep("Edge", v("x"), v("y")), // same binding constraint
		},
	}

	results := Rule(rule, rels)
	// Each edge should match exactly itself once.
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %v", len(results), results)
	}
}

// Test anti-join (negative literal): A(x) ∧ not B(x) → Q(x)
func TestEvalRuleAntiJoin(t *testing.T) {
	A := makeRelation("A", 1, IntVal{1}, IntVal{2}, IntVal{3})
	B := makeRelation("B", 1, IntVal{2})
	rels := map[string]*Relation{"A": A, "B": B}

	rule := plan.PlannedRule{
		Head: head("Q", v("x")),
		JoinOrder: []plan.JoinStep{
			positiveStep("A", v("x")),
			negativeStep("B", v("x")),
		},
	}

	results := Rule(rule, rels)
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
