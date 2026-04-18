package eval

import (
	"context"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// TestApplyComparison_VarVarEq_PropagatesForward exercises the bug fix
// from PR #145 catalog item #1: `var = var` equality with the left side
// bound and the right side unbound must bind the right and emit a row.
//
// Before the fix, applyComparison silently dropped any binding where
// either side was unbound, so a rule like `R(x, y) :- A(x), x = y`
// produced zero results. After the fix, y is bound to x's value and the
// rule produces |A| tuples.
func TestApplyComparison_VarVarEq_PropagatesForward(t *testing.T) {
	cmp := &datalog.Comparison{
		Op:    "=",
		Left:  datalog.Var{Name: "x"},
		Right: datalog.Var{Name: "y"},
	}
	in := []binding{
		{"x": IntVal{1}},
		{"x": IntVal{2}},
		{"x": IntVal{3}},
	}
	out := applyComparison(cmp, in)
	if len(out) != 3 {
		t.Fatalf("expected 3 propagated bindings, got %d: %+v", len(out), out)
	}
	for i, b := range out {
		x, xok := b["x"].(IntVal)
		y, yok := b["y"].(IntVal)
		if !xok || !yok {
			t.Fatalf("row %d: missing x or y: %+v", i, b)
		}
		if x.V != y.V {
			t.Fatalf("row %d: expected x==y, got x=%d y=%d", i, x.V, y.V)
		}
	}
}

// TestApplyComparison_VarVarEq_PropagatesReverse covers right-bound,
// left-unbound — the symmetric case.
func TestApplyComparison_VarVarEq_PropagatesReverse(t *testing.T) {
	cmp := &datalog.Comparison{
		Op:    "=",
		Left:  datalog.Var{Name: "x"},
		Right: datalog.Var{Name: "y"},
	}
	in := []binding{
		{"y": StrVal{"a"}},
		{"y": StrVal{"b"}},
	}
	out := applyComparison(cmp, in)
	if len(out) != 2 {
		t.Fatalf("expected 2 bindings, got %d", len(out))
	}
	for _, b := range out {
		if b["x"] == nil || b["y"] == nil {
			t.Fatalf("expected both x and y bound: %+v", b)
		}
		if b["x"].(StrVal).V != b["y"].(StrVal).V {
			t.Fatalf("expected x==y, got %+v", b)
		}
	}
}

// TestApplyComparison_VarVarEq_NeitherBound is the negative case: when
// neither side is bound the planner has misordered the rule. Eval must
// fail gracefully (empty result, no panic, no fabricated rows).
func TestApplyComparison_VarVarEq_NeitherBound(t *testing.T) {
	cmp := &datalog.Comparison{
		Op:    "=",
		Left:  datalog.Var{Name: "x"},
		Right: datalog.Var{Name: "y"},
	}
	in := []binding{{}, {"z": IntVal{99}}}
	out := applyComparison(cmp, in)
	if len(out) != 0 {
		t.Fatalf("expected 0 bindings (graceful empty), got %d: %+v", len(out), out)
	}
}

// TestApplyComparison_VarVarEq_DoesNotMutateSharedBinding verifies the
// cloned-on-write contract: when applyComparison binds a new variable it
// must not write into the input binding map (which may be shared across
// rows after applyPositive's filter-fast-path).
func TestApplyComparison_VarVarEq_DoesNotMutateSharedBinding(t *testing.T) {
	shared := binding{"x": IntVal{42}}
	in := []binding{shared, shared}
	cmp := &datalog.Comparison{
		Op:    "=",
		Left:  datalog.Var{Name: "x"},
		Right: datalog.Var{Name: "y"},
	}
	out := applyComparison(cmp, in)
	if _, ok := shared["y"]; ok {
		t.Fatalf("input binding mutated; shared map now has y: %+v", shared)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(out))
	}
	for _, b := range out {
		if _, ok := b["y"]; !ok {
			t.Fatalf("output missing y: %+v", b)
		}
	}
}

// TestApplyComparison_VarVarInequality_UnboundDropped pins the
// non-equality behaviour: `x < y` with one side unbound is undefined and
// must drop the row, exactly as before. Binding propagation only applies
// to EqOp.
func TestApplyComparison_VarVarInequality_UnboundDropped(t *testing.T) {
	for _, op := range []string{"!=", "<", ">", "<=", ">="} {
		cmp := &datalog.Comparison{
			Op:    op,
			Left:  datalog.Var{Name: "x"},
			Right: datalog.Var{Name: "y"},
		}
		in := []binding{{"x": IntVal{1}}}
		out := applyComparison(cmp, in)
		if len(out) != 0 {
			t.Fatalf("op %s with unbound y: expected drop, got %+v", op, out)
		}
	}
}

// TestApplyComparison_BothBound_StillFilters is the regression guard:
// the both-bound branch must keep filtering — the fix must not turn `=`
// into a pure binder when both sides are already bound to different
// values.
func TestApplyComparison_BothBound_StillFilters(t *testing.T) {
	cmp := &datalog.Comparison{
		Op:    "=",
		Left:  datalog.Var{Name: "x"},
		Right: datalog.Var{Name: "y"},
	}
	in := []binding{
		{"x": IntVal{1}, "y": IntVal{1}}, // pass
		{"x": IntVal{1}, "y": IntVal{2}}, // drop
		{"x": IntVal{3}, "y": IntVal{3}}, // pass
	}
	out := applyComparison(cmp, in)
	if len(out) != 2 {
		t.Fatalf("expected 2 surviving bindings, got %d: %+v", len(out), out)
	}
}

// TestRule_VarVarEq_EndToEnd_Forward exercises the fix through the full
// Rule evaluator. R(x, y) :- A(x), x = y. Input A = {1, 2, 3}; expected
// output {(1,1), (2,2), (3,3)}.
func TestRule_VarVarEq_EndToEnd_Forward(t *testing.T) {
	A := makeRelation("A", 1, IntVal{1}, IntVal{2}, IntVal{3})
	rels := RelsOf(A)

	rule := plan.PlannedRule{
		Head: datalog.Atom{Predicate: "R", Args: []datalog.Term{
			datalog.Var{Name: "x"}, datalog.Var{Name: "y"},
		}},
		JoinOrder: []plan.JoinStep{
			{Literal: datalog.Literal{
				Positive: true,
				Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{
					datalog.Var{Name: "x"},
				}},
			}},
			{Literal: datalog.Literal{Cmp: &datalog.Comparison{
				Op:    "=",
				Left:  datalog.Var{Name: "x"},
				Right: datalog.Var{Name: "y"},
			}}},
		},
	}
	out, err := Rule(context.Background(), rule, rels, 0)
	if err != nil {
		t.Fatalf("Rule: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("expected 3 head tuples, got %d: %+v", len(out), out)
	}
	for _, tup := range out {
		if tup[0].(IntVal).V != tup[1].(IntVal).V {
			t.Fatalf("expected x==y in head tuple, got %+v", tup)
		}
	}
}

// TestRule_VarVarEq_EndToEnd_Reverse is the symmetric end-to-end test:
// R(x, y) :- A(y), x = y. The y side is bound first; equality must
// propagate to x.
func TestRule_VarVarEq_EndToEnd_Reverse(t *testing.T) {
	A := makeRelation("A", 1, IntVal{10}, IntVal{20})
	rels := RelsOf(A)

	rule := plan.PlannedRule{
		Head: datalog.Atom{Predicate: "R", Args: []datalog.Term{
			datalog.Var{Name: "x"}, datalog.Var{Name: "y"},
		}},
		JoinOrder: []plan.JoinStep{
			{Literal: datalog.Literal{
				Positive: true,
				Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{
					datalog.Var{Name: "y"},
				}},
			}},
			{Literal: datalog.Literal{Cmp: &datalog.Comparison{
				Op:    "=",
				Left:  datalog.Var{Name: "x"},
				Right: datalog.Var{Name: "y"},
			}}},
		},
	}
	out, err := Rule(context.Background(), rule, rels, 0)
	if err != nil {
		t.Fatalf("Rule: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 head tuples, got %d: %+v", len(out), out)
	}
}
