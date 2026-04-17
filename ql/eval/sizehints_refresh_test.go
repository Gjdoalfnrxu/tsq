package eval

import (
	"context"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// TestSizeHintsRefreshUpdatesMap is the unit test required by issue #88: it
// confirms that after a multi-stratum evaluation the sizeHints map contains
// the real materialised tuple counts of the derived predicates produced in
// each stratum (not the defaultSizeHint=1000 fallback).
func TestSizeHintsRefreshUpdatesMap(t *testing.T) {
	// Program:
	//   Tiny(x) :- Seed(x), x = 1.   -- stratum 0, derives 1 tuple from Seed
	//   Out(x)  :- Big(x), Tiny(x).  -- stratum 1, joins a base rel with the IDB
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: datalog.Atom{Predicate: "Tiny", Args: []datalog.Term{v("x")}},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{Predicate: "Seed", Args: []datalog.Term{v("x")}}},
					{Positive: true, Cmp: &datalog.Comparison{Op: "=", Left: v("x"), Right: datalog.IntConst{Value: 1}}},
				},
			},
			// NotInSink(x) :- Big(x), not Sink(x).
			// We define a dummy IDB Sink with no rules of its own — so it
			// stays empty — but the negative edge forces Out to a strictly
			// later stratum than Tiny (the planner stratifies negation).
			{
				Head: datalog.Atom{Predicate: "Sink", Args: []datalog.Term{v("x")}},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{Predicate: "Seed", Args: []datalog.Term{v("x")}}},
					{Positive: true, Cmp: &datalog.Comparison{Op: "=", Left: v("x"), Right: datalog.IntConst{Value: 999}}},
				},
			},
			{
				Head: datalog.Atom{Predicate: "Out", Args: []datalog.Term{v("x")}},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{Predicate: "Big", Args: []datalog.Term{v("x")}}},
					{Positive: true, Atom: datalog.Atom{Predicate: "Tiny", Args: []datalog.Term{v("x")}}},
					// Negative dep on Sink to force a stratum boundary.
					{Positive: false, Atom: datalog.Atom{Predicate: "Sink", Args: []datalog.Term{v("x")}}},
				},
			},
		},
		Query: &datalog.Query{
			Select: []datalog.Term{v("x")},
			Body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Out", Args: []datalog.Term{v("x")}}},
			},
		},
	}

	// Seed has 3 tuples but only x=1 survives the comparison, so Tiny=1.
	seed := makeRelation("Seed", 1, IntVal{1}, IntVal{2}, IntVal{3})
	// Big is a 50-tuple base relation.
	bigVals := make([]Value, 50)
	for i := 0; i < 50; i++ {
		bigVals[i] = IntVal{int64(i)}
	}
	big := makeRelation("Big", 1, bigVals...)
	baseRels := map[string]*Relation{"Seed": seed, "Big": big}

	// Hints reflect what the cmd would supply: only base relations are sized.
	// Tiny falls through to defaultSizeHint=1000.
	hints := map[string]int{"Seed": 3, "Big": 50}

	ep, errs := plan.Plan(prog, hints)
	if len(errs) != 0 {
		t.Fatalf("plan errors: %v", errs)
	}
	if len(ep.Strata) != 2 {
		t.Fatalf("expected 2 strata (Tiny then Out), got %d", len(ep.Strata))
	}

	// Locate the stratum that contains Out (stratum 1).
	outStratumIdx := -1
	for i, s := range ep.Strata {
		for _, r := range s.Rules {
			if r.Head.Predicate == "Out" {
				outStratumIdx = i
			}
		}
	}
	if outStratumIdx == -1 {
		t.Fatalf("Out rule not found in any stratum")
	}

	// Capture the baseline join order for Out *before* eval.
	var baselineOrder []string
	for _, r := range ep.Strata[outStratumIdx].Rules {
		if r.Head.Predicate == "Out" {
			for _, step := range r.JoinOrder {
				baselineOrder = append(baselineOrder, step.Literal.Atom.Predicate)
			}
		}
	}
	// With Tiny unhinted (=1000) and Big=50, planner picks Big first.
	// The negative literal on Sink can only be placed once x is bound, so
	// it lands last regardless. Just check the seed is Big.
	if len(baselineOrder) == 0 || baselineOrder[0] != "Big" {
		t.Fatalf("baseline join order: expected Big first, got %v", baselineOrder)
	}

	// Evaluate with refresh enabled.
	_, err := Evaluate(context.Background(), ep, baseRels, WithSizeHints(hints))
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	// Hint for Tiny must now reflect the real count (1).
	if got := hints["Tiny"]; got != 1 {
		t.Errorf("expected hints[Tiny]=1 after refresh, got %d", got)
	}
	// Out is in stratum 1 and may also have been written. It produced 1 tuple.
	if got := hints["Out"]; got != 1 {
		t.Errorf("expected hints[Out]=1 after refresh, got %d", got)
	}

	// ANTI-GAMING: assert the plan for Out actually swapped after the
	// post-stratum-0 refresh. Tiny=1 < Big=50, so Tiny should be first now.
	var finalOrder []string
	for _, r := range ep.Strata[outStratumIdx].Rules {
		if r.Head.Predicate == "Out" {
			for _, step := range r.JoinOrder {
				finalOrder = append(finalOrder, step.Literal.Atom.Predicate)
			}
		}
	}
	if len(finalOrder) == 0 || finalOrder[0] != "Tiny" {
		t.Errorf("expected Tiny placed first after refresh (size 1 < Big=50), got %v", finalOrder)
	}
}

// TestSizeHintsRefreshNoOptionLeavesPlanUnchanged guards against accidental
// behavioural change for callers that do not opt into refresh. Without
// WithSizeHints, the plan's join orders should be untouched by Evaluate.
func TestSizeHintsRefreshNoOptionLeavesPlanUnchanged(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: datalog.Atom{Predicate: "Tiny", Args: []datalog.Term{v("x")}},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{Predicate: "Seed", Args: []datalog.Term{v("x")}}},
					{Positive: true, Cmp: &datalog.Comparison{Op: "=", Left: v("x"), Right: datalog.IntConst{Value: 1}}},
				},
			},
			{
				Head: datalog.Atom{Predicate: "Sink", Args: []datalog.Term{v("x")}},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{Predicate: "Seed", Args: []datalog.Term{v("x")}}},
					{Positive: true, Cmp: &datalog.Comparison{Op: "=", Left: v("x"), Right: datalog.IntConst{Value: 999}}},
				},
			},
			{
				Head: datalog.Atom{Predicate: "Out", Args: []datalog.Term{v("x")}},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{Predicate: "Big", Args: []datalog.Term{v("x")}}},
					{Positive: true, Atom: datalog.Atom{Predicate: "Tiny", Args: []datalog.Term{v("x")}}},
					{Positive: false, Atom: datalog.Atom{Predicate: "Sink", Args: []datalog.Term{v("x")}}},
				},
			},
		},
	}

	seed := makeRelation("Seed", 1, IntVal{1}, IntVal{2})
	bigVals := make([]Value, 5)
	for i := 0; i < 5; i++ {
		bigVals[i] = IntVal{int64(i)}
	}
	big := makeRelation("Big", 1, bigVals...)
	baseRels := map[string]*Relation{"Seed": seed, "Big": big}

	hints := map[string]int{"Seed": 2, "Big": 5}
	ep, errs := plan.Plan(prog, hints)
	if len(errs) != 0 {
		t.Fatalf("plan errors: %v", errs)
	}

	// Snapshot Out's order pre-eval.
	var pre []string
	for _, s := range ep.Strata {
		for _, r := range s.Rules {
			if r.Head.Predicate == "Out" {
				for _, st := range r.JoinOrder {
					pre = append(pre, st.Literal.Atom.Predicate)
				}
			}
		}
	}

	// Evaluate WITHOUT WithSizeHints → no refresh.
	if _, err := Evaluate(context.Background(), ep, baseRels); err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	var post []string
	for _, s := range ep.Strata {
		for _, r := range s.Rules {
			if r.Head.Predicate == "Out" {
				for _, st := range r.JoinOrder {
					post = append(post, st.Literal.Atom.Predicate)
				}
			}
		}
	}
	if len(pre) != len(post) {
		t.Fatalf("plan length changed: pre=%v post=%v", pre, post)
	}
	for i := range pre {
		if pre[i] != post[i] {
			t.Errorf("plan changed without WithSizeHints: pre=%v post=%v", pre, post)
		}
	}
}
