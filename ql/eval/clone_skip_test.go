package eval

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// TestApplyPositive_CloneSkip_NoVarLeak is the regression test gating
// sub-change (b): when an applyPositive step has zero free variables it
// reuses the input binding directly (no clone). We must prove that
// downstream steps which DO bind new variables don't leak those bindings
// across sibling output rows.
//
// Setup:
//
//	A(x):     {1,2,3}                  // produces 3 input bindings (x=1,2,3)
//	B():      {()} (one zero-arity tup) // pure-filter step, no free vars  ← clone-skip fires
//	C(x,y):   {(1,10),(1,11),(2,20)}   // binds y per row
//
// Expected output for rule H(x,y) :- A(x), B(), C(x,y):
//
//	(1,10), (1,11), (2,20)
//
// If clone-skip leaked the same map across outputs, downstream C would
// stamp y=20 onto an x=1 row (or similar contamination), and we'd see
// fewer/wrong tuples.
func TestApplyPositive_CloneSkip_NoVarLeak(t *testing.T) {
	A := makeRelation("A", 1, IntVal{1}, IntVal{2}, IntVal{3})
	// B is a 0-arity relation with one tuple — every binding satisfies B().
	B := NewRelation("B", 0)
	B.Add(Tuple{})
	C := makeRelation("C", 2,
		IntVal{1}, IntVal{10},
		IntVal{1}, IntVal{11},
		IntVal{2}, IntVal{20},
	)
	rels := RelsOf(A, B, C)

	rule := plan.PlannedRule{
		Head: head("H", v("x"), v("y")),
		JoinOrder: []plan.JoinStep{
			positiveStep("A", v("x")),
			positiveStep("B"), // 0-args — bound/free both empty → clone-skip path
			positiveStep("C", v("x"), v("y")),
		},
	}

	results, err := Rule(rule, rels, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		tupleKey(Tuple{IntVal{1}, IntVal{10}}): true,
		tupleKey(Tuple{IntVal{1}, IntVal{11}}): true,
		tupleKey(Tuple{IntVal{2}, IntVal{20}}): true,
	}
	if len(results) != len(want) {
		t.Fatalf("expected %d tuples, got %d: %v", len(want), len(results), results)
	}
	for _, r := range results {
		if !want[tupleKey(r)] {
			t.Errorf("unexpected tuple (binding leak suspected): %v", r)
		}
	}
}

// TestApplyPositive_CloneSkip_PureFilterEquality covers the case where the
// clone-skipped step binds NO new vars but DOES filter on a constant. The
// filter must reject non-matching rows without affecting downstream binding
// state.
//
// A(x): {1,2,3}; B(2) — only x=2 should survive; C(x,y) extends.
func TestApplyPositive_CloneSkip_PureFilterEquality(t *testing.T) {
	A := makeRelation("A", 1, IntVal{1}, IntVal{2}, IntVal{3})
	B := makeRelation("B", 1, IntVal{2}) // single fact: B(2)
	C := makeRelation("C", 2,
		IntVal{1}, IntVal{10},
		IntVal{2}, IntVal{20},
		IntVal{2}, IntVal{21},
		IntVal{3}, IntVal{30},
	)
	rels := RelsOf(A, B, C)

	// JoinOrder: A(x), B(x), C(x,y).
	// At the B(x) step, x is already bound from A — boundCols=[0],
	// freeVars=[] → clone-skip path triggers.
	rule := plan.PlannedRule{
		Head: head("H", v("x"), v("y")),
		JoinOrder: []plan.JoinStep{
			positiveStep("A", v("x")),
			positiveStep("B", v("x")),
			positiveStep("C", v("x"), v("y")),
		},
	}

	results, err := Rule(rule, rels, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		tupleKey(Tuple{IntVal{2}, IntVal{20}}): true,
		tupleKey(Tuple{IntVal{2}, IntVal{21}}): true,
	}
	if len(results) != len(want) {
		t.Fatalf("expected %d tuples, got %d: %v", len(want), len(results), results)
	}
	for _, r := range results {
		if !want[tupleKey(r)] {
			t.Errorf("unexpected tuple: %v", r)
		}
	}
}

// TestApplyPositive_CloneSkip_RepeatedConstFilter exercises the
// many-matches-per-input case. B(_) matches every B-tuple; for each input
// binding we get N output rows that all share the same map. Downstream C
// must extend each independently without writing through to siblings.
//
// A(x): {1,2}; B(_,_) has 5 tuples — wildcard probe → 5 matches per input
// → applyPositive emits 2*5=10 rows pointing at one of two underlying
// maps (one per A binding). C(x,y) then extends them.
//
// If shared-map mutation leaked, x=1 and x=2 would get crossed over and
// the result count would be wrong.
func TestApplyPositive_CloneSkip_RepeatedConstFilter(t *testing.T) {
	A := makeRelation("A", 1, IntVal{1}, IntVal{2})
	B := NewRelation("B", 2)
	for i := 0; i < 5; i++ {
		B.Add(Tuple{IntVal{int64(i)}, IntVal{int64(i + 100)}})
	}
	C := makeRelation("C", 2,
		IntVal{1}, IntVal{10},
		IntVal{2}, IntVal{20},
	)
	rels := RelsOf(A, B, C)

	w := datalog.Var{Name: "_"}
	rule := plan.PlannedRule{
		Head: head("H", v("x"), v("y")),
		JoinOrder: []plan.JoinStep{
			positiveStep("A", v("x")),
			positiveStep("B", w, w), // both wildcards → no bound, no free → clone-skip
			positiveStep("C", v("x"), v("y")),
		},
	}

	results, err := Rule(rule, rels, 0)
	if err != nil {
		t.Fatal(err)
	}
	// After projection through head H(x,y) with set semantics, dedup
	// collapses identical (x,y) tuples even though the join produced
	// 5 copies of each. Expected unique outputs: (1,10) and (2,20).
	want := map[string]bool{
		tupleKey(Tuple{IntVal{1}, IntVal{10}}): true,
		tupleKey(Tuple{IntVal{2}, IntVal{20}}): true,
	}
	seen := map[string]int{}
	for _, r := range results {
		seen[tupleKey(r)]++
		if !want[tupleKey(r)] {
			t.Errorf("unexpected tuple (binding leak suspected): %v", r)
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("expected %d distinct tuples, got %d: %v", len(want), len(seen), results)
	}
}
