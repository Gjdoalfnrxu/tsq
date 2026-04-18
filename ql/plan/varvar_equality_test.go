package plan

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// TestOrderJoins_VarVarEq_OrderedAfterBindingStep verifies that the
// planner places `x = y` only after some step has bound at least one
// side. Combined with applyComparison's propagation, this means the
// equality acts as a binding step rather than dropping rows.
//
// Bug ref: PR #145 catalog item #1.
func TestOrderJoins_VarVarEq_OrderedAfterBindingStep(t *testing.T) {
	body := []datalog.Literal{
		{Cmp: &datalog.Comparison{
			Op:    "=",
			Left:  datalog.Var{Name: "x"},
			Right: datalog.Var{Name: "y"},
		}},
		{Positive: true, Atom: datalog.Atom{
			Predicate: "A",
			Args:      []datalog.Term{datalog.Var{Name: "x"}},
		}},
	}
	steps := orderJoins(body, nil)
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}
	// The first step must be the A(x) atom — equality cannot lead.
	if steps[0].Literal.Cmp != nil {
		t.Fatalf("equality placed first; expected A(x) first: %+v", steps)
	}
	if steps[1].Literal.Cmp == nil {
		t.Fatalf("equality not placed second: %+v", steps)
	}
}

// TestOrderJoins_VarVarEq_EligibleWithOneBoundSide is the unit-level
// gate on isEligible: equality with one side bound must now be eligible
// (it acts as a binder). Pre-fix it was rejected, leading to "no
// eligible literal" planner stalls or unsolvable rules.
func TestOrderJoins_VarVarEq_EligibleWithOneBoundSide(t *testing.T) {
	bound := map[string]bool{"x": true}
	lit := datalog.Literal{Cmp: &datalog.Comparison{
		Op:    "=",
		Left:  datalog.Var{Name: "x"},
		Right: datalog.Var{Name: "y"},
	}}
	if !isEligible(lit, bound) {
		t.Fatalf("var=var equality with left bound should be eligible")
	}
	// Symmetric.
	bound2 := map[string]bool{"y": true}
	if !isEligible(lit, bound2) {
		t.Fatalf("var=var equality with right bound should be eligible")
	}
	// Neither bound: still ineligible.
	if isEligible(lit, map[string]bool{}) {
		t.Fatalf("var=var equality with neither bound must remain ineligible")
	}
}

// TestOrderJoins_VarVarInequality_RequiresBothBound is the regression
// guard: the new isEligible relaxation applies ONLY to EqOp. `x < y`
// with one side unbound must remain ineligible.
func TestOrderJoins_VarVarInequality_RequiresBothBound(t *testing.T) {
	for _, op := range []string{"!=", "<", ">", "<=", ">="} {
		bound := map[string]bool{"x": true}
		lit := datalog.Literal{Cmp: &datalog.Comparison{
			Op:    op,
			Left:  datalog.Var{Name: "x"},
			Right: datalog.Var{Name: "y"},
		}}
		if isEligible(lit, bound) {
			t.Fatalf("op %s with unbound y must be ineligible", op)
		}
	}
}
