package eval

import (
	"context"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// TestApplyNegative_FastPath_NoRedundantReequality is the gate test for
// issue #97 (mirror of #89 / PR #94 on the positive side).
//
// applyNegative trusts Index.Lookup: if the index returns any candidate
// idxs, the binding is rejected (anti-join fails). The previous code did
// a per-tuple Compare("=", ...) re-check inside the loop — dead work,
// since boundCols is built in ascending order from atom.Args and
// Index.Lookup keys are canonical (see partialkey_canonicality_test.go).
//
// This test asserts the fast path's correctness on a hot anti-join input:
// a relation with many candidates per probe, all of which match. If a
// future change reintroduces the redundant re-check the test still passes
// behaviourally — the canonicality contract guarantees it would. The
// real mutation guard is the partialKey/Index agreement tests; this test
// pins the public behaviour of applyNegative on the relevant shape so
// any logic regression (e.g. dropping the lookup, reversing a condition)
// fails loudly.
func TestApplyNegative_FastPath_NoRedundantReequality(t *testing.T) {
	// R(x,y) has many y-rows for x=1 (the hot-anti-join shape: one
	// probe value, many candidate matches). Probing not R(1, _) must
	// reject any binding with x=1.
	R := NewRelation("R", 2)
	for i := 0; i < 64; i++ {
		R.Add(Tuple{IntVal{1}, IntVal{int64(i)}})
	}
	R.Add(Tuple{IntVal{2}, IntVal{0}})
	R.Add(Tuple{IntVal{3}, IntVal{0}})

	// Input bindings: x=1 (must be filtered out — many R matches),
	// x=99 (must survive — no R matches).
	bindings := []binding{
		{"x": IntVal{1}},
		{"x": IntVal{99}},
	}
	atom := datalog.Atom{
		Predicate: "R",
		Args: []datalog.Term{
			datalog.Var{Name: "x"},
			datalog.Wildcard{},
		},
	}
	rels := RelsOf(R)

	out := applyNegative(atom, rels, bindings)
	if len(out) != 1 {
		t.Fatalf("expected 1 surviving binding, got %d: %+v", len(out), out)
	}
	got, ok := out[0]["x"].(IntVal)
	if !ok || got.V != 99 {
		t.Fatalf("expected x=99 to survive (no R match); got %+v", out[0])
	}
}

// TestApplyNegative_FastPath_MultiBoundCols ensures the fast path is
// correct when multiple columns are bound — the regime where the dropped
// re-check might have masked an Index.Lookup bug. With sorted boundCols
// (which is what applyNegative always builds), Lookup is exact.
func TestApplyNegative_FastPath_MultiBoundCols(t *testing.T) {
	R := NewRelation("R", 3)
	R.Add(Tuple{IntVal{1}, StrVal{"a"}, IntVal{10}})
	R.Add(Tuple{IntVal{1}, StrVal{"b"}, IntVal{10}})
	R.Add(Tuple{IntVal{2}, StrVal{"a"}, IntVal{10}})

	// Bindings: (x=1,z=10) → matches rows 0 and 1, rejected.
	//           (x=1,z=99) → no match, survives.
	//           (x=9,z=10) → no match, survives.
	bindings := []binding{
		{"x": IntVal{1}, "z": IntVal{10}},
		{"x": IntVal{1}, "z": IntVal{99}},
		{"x": IntVal{9}, "z": IntVal{10}},
	}
	atom := datalog.Atom{
		Predicate: "R",
		Args: []datalog.Term{
			datalog.Var{Name: "x"},
			datalog.Wildcard{},
			datalog.Var{Name: "z"},
		},
	}
	rels := RelsOf(R)

	out := applyNegative(atom, rels, bindings)
	if len(out) != 2 {
		t.Fatalf("expected 2 surviving bindings, got %d: %+v", len(out), out)
	}
}

// TestApplyNegative_EndToEnd_AntiJoin runs the negative path through the
// full Rule evaluator to catch any wiring regression.
//
// Rule: H(x) :- A(x), not B(x).
// A = {1,2,3}; B = {2}. Expected output: {1, 3}.
func TestApplyNegative_EndToEnd_AntiJoin(t *testing.T) {
	A := makeRelation("A", 1, IntVal{1}, IntVal{2}, IntVal{3})
	B := makeRelation("B", 1, IntVal{2})
	rels := RelsOf(A, B)

	rule := plan.PlannedRule{
		Head: datalog.Atom{Predicate: "H", Args: []datalog.Term{datalog.Var{Name: "x"}}},
		JoinOrder: []plan.JoinStep{
			{Literal: datalog.Literal{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}}}}},
			{Literal: datalog.Literal{Positive: false, Atom: datalog.Atom{Predicate: "B", Args: []datalog.Term{datalog.Var{Name: "x"}}}}},
		},
	}

	out, err := Rule(context.Background(), rule, rels, 0)
	if err != nil {
		t.Fatalf("Rule: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 head tuples, got %d: %+v", len(out), out)
	}
	seen := map[int64]bool{}
	for _, tup := range out {
		seen[tup[0].(IntVal).V] = true
	}
	if !seen[1] || !seen[3] || seen[2] {
		t.Fatalf("expected {1,3}, got %+v", out)
	}
}
