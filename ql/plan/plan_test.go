package plan_test

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

func TestPlaceholder(t *testing.T) {}

// TestPlanRetainsBody verifies that PlannedRule.Body is populated so that
// RePlanStratum has the input it needs to recompute join orders. Issue #88.
func TestPlanRetainsBody(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: atom("P", "x"),
				Body: []datalog.Literal{posLit("A", "x"), posLit("B", "x")},
			},
		},
	}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(ep.Strata) == 0 || len(ep.Strata[0].Rules) == 0 {
		t.Fatal("no rules planned")
	}
	r := ep.Strata[0].Rules[0]
	if len(r.Body) != 2 {
		t.Fatalf("expected Body to have 2 literals, got %d", len(r.Body))
	}
	if r.Body[0].Atom.Predicate != "A" || r.Body[1].Atom.Predicate != "B" {
		t.Errorf("expected Body to preserve source order [A,B], got [%s,%s]",
			r.Body[0].Atom.Predicate, r.Body[1].Atom.Predicate)
	}
}

// TestRePlanStratumSwapsJoinOrder is the anti-gaming test required by
// issue #88. It does not just check that the hints map updates — it asserts
// that the **plan changes** in the expected direction when an IDB hint is
// refreshed from defaultSizeHint=1000 down to its real (small) cardinality.
//
// Setup: a rule P(x) :- A(x), B(x) where A is a base relation with 100
// tuples and B is an IDB. With B unhinted, B scores as 1000 and A is
// placed first. After we refresh hints with B=5, B should be placed first.
func TestRePlanStratumSwapsJoinOrder(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: atom("P", "x"),
				Body: []datalog.Literal{posLit("A", "x"), posLit("B", "x")},
			},
		},
	}
	// First plan: only A's size is known. B will fall through to default 1000.
	hints := map[string]int{"A": 100}
	ep, errs := plan.Plan(prog, hints)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	r := ep.Strata[0].Rules[0]
	if r.JoinOrder[0].Literal.Atom.Predicate != "A" {
		t.Fatalf("baseline: expected A first (size 100 < default 1000 for B), got %s",
			r.JoinOrder[0].Literal.Atom.Predicate)
	}

	// Refresh: B turns out to have 5 tuples. Re-plan should swap B to first.
	hints["B"] = 5
	plan.RePlanStratum(&ep.Strata[0], hints)

	r = ep.Strata[0].Rules[0]
	if r.JoinOrder[0].Literal.Atom.Predicate != "B" {
		t.Errorf("after refresh: expected B first (size 5 < A=100), got %s",
			r.JoinOrder[0].Literal.Atom.Predicate)
	}
	if r.JoinOrder[1].Literal.Atom.Predicate != "A" {
		t.Errorf("after refresh: expected A second, got %s",
			r.JoinOrder[1].Literal.Atom.Predicate)
	}
}

// TestRePlanStratumNoOpWhenBodyMissing verifies legacy callers that did not
// populate Body are not affected — RePlanStratum should be a no-op for such
// rules rather than crash.
func TestRePlanStratumNoOpWhenBodyMissing(t *testing.T) {
	s := &plan.Stratum{
		Rules: []plan.PlannedRule{
			{
				Head: atom("P", "x"),
				// Body intentionally nil.
				JoinOrder: []plan.JoinStep{
					{Literal: posLit("A", "x")},
				},
			},
		},
	}
	plan.RePlanStratum(s, map[string]int{"A": 1})
	if len(s.Rules[0].JoinOrder) != 1 {
		t.Errorf("expected JoinOrder unchanged for nil-Body rule, got %d steps",
			len(s.Rules[0].JoinOrder))
	}
	if s.Rules[0].JoinOrder[0].Literal.Atom.Predicate != "A" {
		t.Errorf("expected JoinOrder unchanged, got %s",
			s.Rules[0].JoinOrder[0].Literal.Atom.Predicate)
	}
}

// TestRePlanQuerySwapsJoinOrder mirrors the rule-level test for the final
// query (which has no separate Body field — the literals live in the
// JoinOrder itself and are reconstructed for re-planning).
func TestRePlanQuerySwapsJoinOrder(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: atom("B", "x"),
				Body: []datalog.Literal{posLit("Seed", "x")},
			},
		},
		Query: &datalog.Query{
			Select: []datalog.Term{datalog.Var{Name: "x"}},
			Body:   []datalog.Literal{posLit("A", "x"), posLit("B", "x")},
		},
	}
	hints := map[string]int{"A": 100, "Seed": 1}
	ep, errs := plan.Plan(prog, hints)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if ep.Query == nil {
		t.Fatal("expected a planned query")
	}
	if ep.Query.JoinOrder[0].Literal.Atom.Predicate != "A" {
		t.Fatalf("baseline: expected A first, got %s",
			ep.Query.JoinOrder[0].Literal.Atom.Predicate)
	}

	hints["B"] = 5
	plan.RePlanQuery(ep.Query, hints)
	if ep.Query.JoinOrder[0].Literal.Atom.Predicate != "B" {
		t.Errorf("after refresh: expected B first (size 5), got %s",
			ep.Query.JoinOrder[0].Literal.Atom.Predicate)
	}
}
