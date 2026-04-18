package plan_test

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// TestIsClassExtentBody verifies the structural detector accepts the
// canonical class-extent shape and rejects bodies that would force eager
// materialisation of multiple large extents or unsafe constructs.
func TestIsClassExtentBody(t *testing.T) {
	base := map[string]bool{"Symbol": true, "Node": true, "Foo": true}
	mat := map[string]bool{"AlreadyMat": true}

	cases := []struct {
		name string
		body []datalog.Literal
		want bool
	}{
		{
			name: "single base atom — accept",
			body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Symbol", Args: []datalog.Term{datalog.Var{Name: "this"}, datalog.Wildcard{}, datalog.Wildcard{}, datalog.Wildcard{}}}},
			},
			want: true,
		},
		{
			name: "base atom plus comparison — accept",
			body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Symbol", Args: []datalog.Term{datalog.Var{Name: "this"}, datalog.Wildcard{}, datalog.Wildcard{}, datalog.Wildcard{}}}},
				{Positive: true, Cmp: &datalog.Comparison{Op: ">", Left: datalog.Var{Name: "this"}, Right: datalog.IntConst{Value: 0}}},
			},
			want: true,
		},
		{
			name: "atom over already-materialised extent — accept",
			body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "AlreadyMat", Args: []datalog.Term{datalog.Var{Name: "this"}}}},
			},
			want: true,
		},
		{
			name: "atom over unknown predicate — reject",
			body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "UnknownIDB", Args: []datalog.Term{datalog.Var{Name: "this"}}}},
			},
			want: false,
		},
		{
			name: "negation in body — reject",
			body: []datalog.Literal{
				{Positive: true, Atom: datalog.Atom{Predicate: "Symbol", Args: []datalog.Term{datalog.Var{Name: "this"}, datalog.Wildcard{}, datalog.Wildcard{}, datalog.Wildcard{}}}},
				{Positive: false, Atom: datalog.Atom{Predicate: "Foo", Args: []datalog.Term{datalog.Var{Name: "this"}}}},
			},
			want: false,
		},
		{
			name: "aggregate sub-goal — reject",
			body: []datalog.Literal{
				{Positive: true, Agg: &datalog.Aggregate{Func: "count"}},
			},
			want: false,
		},
		{
			name: "empty body — reject (degenerate)",
			body: nil,
			want: false,
		},
		{
			name: "only comparisons (no positive atom) — reject",
			body: []datalog.Literal{
				{Positive: true, Cmp: &datalog.Comparison{Op: "=", Left: datalog.Var{Name: "this"}, Right: datalog.IntConst{Value: 1}}},
			},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := plan.IsClassExtentBody(tc.body, base, mat)
			if got != tc.want {
				t.Errorf("IsClassExtentBody = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestEstimateAndPlanWithExtents_StripsMaterialisedRules verifies that
// when the materialising hook flags a head as materialised, the planner
// removes that rule from the program before stratification (so no plan
// stratum re-evaluates it).
func TestEstimateAndPlanWithExtents_StripsMaterialisedRules(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head:        datalog.Atom{Predicate: "ClassA", Args: []datalog.Term{datalog.Var{Name: "this"}}},
				Body:        []datalog.Literal{{Positive: true, Atom: datalog.Atom{Predicate: "BaseRel", Args: []datalog.Term{datalog.Var{Name: "this"}, datalog.Wildcard{}}}}},
				ClassExtent: true,
			},
			{
				// Unrelated rule — must NOT be stripped.
				Head: datalog.Atom{Predicate: "OtherIDB", Args: []datalog.Term{datalog.Var{Name: "x"}}},
				Body: []datalog.Literal{{Positive: true, Atom: datalog.Atom{Predicate: "BaseRel", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Wildcard{}}}}},
			},
		},
	}

	matExtHook := func(prog *datalog.Program, sizeHints map[string]int, _ int) map[string]bool {
		// Pretend we materialised ClassA.
		sizeHints["ClassA"] = 7
		return map[string]bool{"ClassA": true}
	}

	var seenProg *datalog.Program
	planFn := func(p *datalog.Program, _ map[string]int) (*plan.ExecutionPlan, []error) {
		seenProg = p
		// Return a non-nil plan so EstimateAndPlanWithExtents doesn't
		// surface a planning error to the caller.
		return &plan.ExecutionPlan{}, nil
	}

	_, errs := plan.EstimateAndPlanWithExtents(prog, nil, 0, nil, matExtHook, planFn)
	if len(errs) > 0 {
		t.Fatalf("unexpected plan errors: %v", errs)
	}
	if seenProg == nil {
		t.Fatal("planFn was not invoked")
	}
	for _, rule := range seenProg.Rules {
		if rule.Head.Predicate == "ClassA" {
			t.Errorf("ClassA rule was not stripped from planned program")
		}
	}
	// OtherIDB must still be there.
	found := false
	for _, rule := range seenProg.Rules {
		if rule.Head.Predicate == "OtherIDB" {
			found = true
		}
	}
	if !found {
		t.Errorf("OtherIDB rule was incorrectly stripped")
	}
}

// TestEstimateAndPlanWithExtents_DoesNotStripUntaggedHeads verifies the
// safety guard: even if the hook returns a name in materialisedExtents,
// rules that are NOT ClassExtent-tagged (or have arity != 1) are left
// alone. This protects against a hand-written predicate that happens to
// share a class extent's name from being silently dropped.
func TestEstimateAndPlanWithExtents_DoesNotStripUntaggedHeads(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				// Same name as the materialised extent, but NOT tagged
				// (would happen if an upstream pass rewrote a rule and
				// lost the tag, or if a user predicate clashes).
				Head: datalog.Atom{Predicate: "ClassA", Args: []datalog.Term{datalog.Var{Name: "this"}}},
				Body: []datalog.Literal{{Positive: true, Atom: datalog.Atom{Predicate: "BaseRel", Args: []datalog.Term{datalog.Var{Name: "this"}, datalog.Wildcard{}}}}},
				// ClassExtent: false (zero value).
			},
		},
	}
	matExtHook := func(prog *datalog.Program, sizeHints map[string]int, _ int) map[string]bool {
		return map[string]bool{"ClassA": true}
	}
	var seenProg *datalog.Program
	planFn := func(p *datalog.Program, _ map[string]int) (*plan.ExecutionPlan, []error) {
		seenProg = p
		return &plan.ExecutionPlan{}, nil
	}
	_, _ = plan.EstimateAndPlanWithExtents(prog, nil, 0, nil, matExtHook, planFn)

	if len(seenProg.Rules) != 1 || seenProg.Rules[0].Head.Predicate != "ClassA" {
		t.Errorf("untagged ClassA rule should be preserved; got rules: %v", seenProg.Rules)
	}
}
