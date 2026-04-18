package plan_test

import (
	"reflect"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// liveVarsAt returns the LiveVars at step i of the first rule's plan, as a
// set for stable comparison.
func liveVarsAt(t *testing.T, ep *plan.ExecutionPlan, i int) map[string]bool {
	t.Helper()
	if len(ep.Strata) == 0 || len(ep.Strata[0].Rules) == 0 {
		t.Fatal("no rules in plan")
	}
	r := ep.Strata[0].Rules[0]
	if i < 0 || i >= len(r.JoinOrder) {
		t.Fatalf("step %d out of range (have %d steps)", i, len(r.JoinOrder))
	}
	out := map[string]bool{}
	for _, v := range r.JoinOrder[i].LiveVars {
		out[v] = true
	}
	return out
}

func eqSet(a map[string]bool, want ...string) bool {
	if len(a) != len(want) {
		return false
	}
	for _, w := range want {
		if !a[w] {
			return false
		}
	}
	return true
}

// TestProjectionPushdown_ChainJoin is the load-bearing test from the spec.
//
// Body: A(x,y), B(y,z), C(z,w). Head: R(x,w).
// After step 1 (A⨝B), only {x, z} should survive (y is dead).
// After step 2 (joining C), only {x, w} should survive (z is dead).
// After step 3 (last step), only {x, w} (the head vars).
func TestProjectionPushdown_ChainJoin(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("R", "x", "w"),
				posLit("A", "x", "y"),
				posLit("B", "y", "z"),
				posLit("C", "z", "w"),
			),
		},
	}
	// Equal sizeHints so greedy ordering picks them in source order.
	hints := map[string]int{"A": 100, "B": 100, "C": 100}
	ep, errs := plan.Plan(prog, hints)
	if len(errs) != 0 {
		t.Fatalf("plan errors: %v", errs)
	}
	r := ep.Strata[0].Rules[0]
	if len(r.JoinOrder) != 3 {
		t.Fatalf("want 3 steps, got %d", len(r.JoinOrder))
	}
	// Order is A, B, C (greedy sticks with source on equal hints).
	if got := r.JoinOrder[0].Literal.Atom.Predicate; got != "A" {
		t.Fatalf("step 0 want A, got %s", got)
	}
	if got := r.JoinOrder[1].Literal.Atom.Predicate; got != "B" {
		t.Fatalf("step 1 want B, got %s", got)
	}
	if got := r.JoinOrder[2].Literal.Atom.Predicate; got != "C" {
		t.Fatalf("step 2 want C, got %s", got)
	}
	// After A: B needs y,z; C needs z,w; head needs x,w. Live = {x,y}? No —
	// we want what survives AFTER step 0. B uses {y,z} so y is needed; head
	// needs x, C needs {z,w}. So after A, live = {x, y} (z and w not yet
	// bound but referenced by later steps; we keep what's bound — only
	// x and y are bound at this point AND in the demand set).
	// LiveVars is the set of CURRENTLY-BOUND vars to KEEP. After step 0, y
	// must survive because B needs it; x must survive because head needs it.
	// z, w are not yet bound — they only enter the binding when their step
	// runs. So after step 0: live = {x, y}.
	if got := liveVarsAt(t, ep, 0); !eqSet(got, "x", "y") {
		t.Errorf("after step 0 (A): want {x,y}, got %v", got)
	}
	// After step 1 (A⨝B): bindings hold {x, y, z}. Downstream (C, head)
	// needs {z, w} ∪ {x, w} = {x, z, w}. y is now dead (not in C, not in
	// head). So keep {x, z}.
	if got := liveVarsAt(t, ep, 1); !eqSet(got, "x", "z") {
		t.Errorf("after step 1 (B): want {x,z}, got %v", got)
	}
	// After step 2 (last step): only head vars {x, w}.
	if got := liveVarsAt(t, ep, 2); !eqSet(got, "x", "w") {
		t.Errorf("after step 2 (C): want {x,w}, got %v", got)
	}
}

// TestProjectionPushdown_StarJoin: star-shape, all branches share a hub var.
// Body: A(h,x), B(h,y), C(h,z). Head: R(x,y,z).
// All three use h, but head doesn't. So h survives intermediate steps but
// is dropped at the end.
func TestProjectionPushdown_StarJoin(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("R", "x", "y", "z"),
				posLit("A", "h", "x"),
				posLit("B", "h", "y"),
				posLit("C", "h", "z"),
			),
		},
	}
	hints := map[string]int{"A": 100, "B": 100, "C": 100}
	ep, errs := plan.Plan(prog, hints)
	if len(errs) != 0 {
		t.Fatalf("plan errors: %v", errs)
	}
	_ = ep.Strata[0].Rules[0]
	// After step 0 (A): bound = {h, x}. Downstream needs h (B,C),
	// y/z (head, B, C), x (head). So keep {h, x}.
	if got := liveVarsAt(t, ep, 0); !eqSet(got, "h", "x") {
		t.Errorf("after step 0: want {h,x}, got %v", got)
	}
	// After step 1 (B): bound = {h, x, y}. Downstream needs h (C),
	// z (head, C), x and y (head). Keep {h, x, y}.
	if got := liveVarsAt(t, ep, 1); !eqSet(got, "h", "x", "y") {
		t.Errorf("after step 1: want {h,x,y}, got %v", got)
	}
	// After step 2 (last): head = {x,y,z}. h is dropped.
	if got := liveVarsAt(t, ep, 2); !eqSet(got, "x", "y", "z") {
		t.Errorf("after step 2: want {x,y,z}, got %v", got)
	}
}

// TestProjectionPushdown_HeadEqualsAllBodyVars: when head names every body
// var, the final-step LiveVars equals all bound vars and projection cannot
// drop anything at the last step.
func TestProjectionPushdown_HeadEqualsAllBodyVars(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("R", "x", "y", "z"),
				posLit("A", "x", "y"),
				posLit("B", "y", "z"),
			),
		},
	}
	ep, errs := plan.Plan(prog, map[string]int{"A": 100, "B": 100})
	if len(errs) != 0 {
		t.Fatalf("plan errors: %v", errs)
	}
	// Last step LiveVars = {x,y,z}.
	last := ep.Strata[0].Rules[0].JoinOrder[1]
	gotLast := map[string]bool{}
	for _, v := range last.LiveVars {
		gotLast[v] = true
	}
	if !eqSet(gotLast, "x", "y", "z") {
		t.Errorf("last step LiveVars want {x,y,z}, got %v", gotLast)
	}
}

// TestProjectionPushdown_SingleStepBody: body of length 1 — last (and only)
// step's LiveVars = head vars.
func TestProjectionPushdown_SingleStepBody(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("R", "x"), posLit("A", "x", "y")),
		},
	}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("plan errors: %v", errs)
	}
	got := ep.Strata[0].Rules[0].JoinOrder[0].LiveVars
	if !reflect.DeepEqual(got, []string{"x"}) {
		t.Errorf("single-step LiveVars want [x], got %v", got)
	}
}

// TestProjectionPushdown_ComparisonDropsOperandAfter: comparison filter
// uses operand vars at its step, but if neither operand is referenced
// downstream, both are dropped after.
func TestProjectionPushdown_ComparisonDropsOperandAfter(t *testing.T) {
	// R(x) :- A(x,y), C(z,x), y = z.
	// Greedy ordering will likely place A first (no bound vars), then C
	// (shares x), then the comparison (both operands now bound). After cmp
	// (last step), only x survives.
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("R", "x"),
				posLit("A", "x", "y"),
				posLit("C", "z", "x"),
				cmpLit("=", "y", "z"),
			),
		},
	}
	hints := map[string]int{"A": 100, "C": 100}
	ep, errs := plan.Plan(prog, hints)
	if len(errs) != 0 {
		t.Fatalf("plan errors: %v", errs)
	}
	r := ep.Strata[0].Rules[0]
	// Find the comparison step.
	cmpIdx := -1
	for i, s := range r.JoinOrder {
		if s.Literal.Cmp != nil {
			cmpIdx = i
		}
	}
	if cmpIdx < 0 {
		t.Fatalf("could not find comparison step in %+v", r.JoinOrder)
	}
	// After the comparison step, neither y nor z should remain in LiveVars
	// — they are pure filter operands once consumed by the comparison and
	// nothing downstream (or the head, which is just {x}) needs them.
	cmpLive := map[string]bool{}
	for _, v := range r.JoinOrder[cmpIdx].LiveVars {
		cmpLive[v] = true
	}
	if cmpLive["y"] {
		t.Errorf("after comparison step, y should be dropped; got %v", cmpLive)
	}
	if cmpLive["z"] {
		t.Errorf("after comparison step, z should be dropped; got %v", cmpLive)
	}
}

// TestProjectionPushdown_NegationKeepsVarsAtItsStep: a negative literal
// requires its vars at evaluation time, so they must be in the live set
// at the step IMMEDIATELY before it. After the negation runs, if no
// further step needs them, they may be dropped.
func TestProjectionPushdown_NegationKeepsVarsAtItsStep(t *testing.T) {
	// R(x) :- A(x,y), B(x,z), !N(y,z).
	// !N requires y,z bound — so steps before !N must keep y, z.
	// After !N (last step), only x survives.
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("R", "x"),
				posLit("A", "x", "y"),
				posLit("B", "x", "z"),
				negLit("N", "y", "z"),
			),
		},
	}
	hints := map[string]int{"A": 100, "B": 100, "N": 100}
	ep, errs := plan.Plan(prog, hints)
	if len(errs) != 0 {
		t.Fatalf("plan errors: %v", errs)
	}
	r := ep.Strata[0].Rules[0]
	if len(r.JoinOrder) != 3 {
		t.Fatalf("want 3 steps, got %d", len(r.JoinOrder))
	}
	// The negation must be last (its vars y and z are bound by A and B).
	if r.JoinOrder[2].Literal.Positive {
		t.Fatalf("expected negative literal last, got %+v", r.JoinOrder[2].Literal)
	}
	// Before negation runs (i.e. LiveVars at step 1, the second positive
	// atom), y and z must both be live.
	live1 := map[string]bool{}
	for _, v := range r.JoinOrder[1].LiveVars {
		live1[v] = true
	}
	if !live1["y"] || !live1["z"] {
		t.Errorf("step 1 must keep y and z for downstream negation; got %v", live1)
	}
	// After the negation (last step), only head vars.
	last := r.JoinOrder[2].LiveVars
	if !reflect.DeepEqual(last, []string{"x"}) {
		t.Errorf("last step LiveVars want [x], got %v", last)
	}
}

// TestProjectionPushdown_AggregateInBodyDoesNotCrash: an aggregate literal
// in the rule body is a no-op at the per-step join level (its own body is
// evaluated separately by Aggregate()). Projection pushdown must not
// crash on it. The aggregate's result var is not bound by the join step
// itself — it enters via the result-relation channel — so it does not
// appear in LiveVars from the projection's perspective. The point of
// this test is that varsInLiteral handles agg literals safely and
// computeLiveVars produces a stable plan.
func TestProjectionPushdown_AggregateInBodyDoesNotCrash(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("R", "c"),
				posLit("A", "x"),
				aggLit("count", "A", "x", "c"),
			),
		},
	}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("plan errors: %v", errs)
	}
	r := ep.Strata[0].Rules[0]
	for i, step := range r.JoinOrder {
		if step.LiveVars == nil {
			t.Errorf("step %d LiveVars is nil — projection pushdown skipped", i)
		}
	}
}

// TestProjectionPushdown_QuerySelectVarsHonoured: query Select determines
// the final LiveVars of the query plan.
func TestProjectionPushdown_QuerySelectVarsHonoured(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("R", "x", "y"), posLit("A", "x", "y")),
		},
		Query: &datalog.Query{
			Select: []datalog.Term{datalog.Var{Name: "x"}},
			Body:   []datalog.Literal{posLit("R", "x", "y")},
		},
	}
	ep, errs := plan.Plan(prog, nil)
	if len(errs) != 0 {
		t.Fatalf("plan errors: %v", errs)
	}
	if ep.Query == nil || len(ep.Query.JoinOrder) == 0 {
		t.Fatal("no query plan")
	}
	last := ep.Query.JoinOrder[len(ep.Query.JoinOrder)-1].LiveVars
	if !reflect.DeepEqual(last, []string{"x"}) {
		t.Errorf("query last-step LiveVars want [x], got %v", last)
	}
}

// TestProjectionPushdown_RecursiveRule: recursive rule (Path) — projection
// must respect that recursive references count as downstream literals.
// Path(x,z) :- Path(x,y), Edge(y,z). Head needs x,z.
// After Path step (assume it's first): bound {x,y}; downstream Edge needs
// y,z; head needs x,z. So live = {x,y}.
// After Edge: head only — {x,z}.
func TestProjectionPushdown_RecursiveRule(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("Path", "x", "z"),
				posLit("Path", "x", "y"),
				posLit("Edge", "y", "z"),
			),
		},
	}
	hints := map[string]int{"Path": 100, "Edge": 100}
	ep, errs := plan.Plan(prog, hints)
	if len(errs) != 0 {
		t.Fatalf("plan errors: %v", errs)
	}
	r := ep.Strata[0].Rules[0]
	if len(r.JoinOrder) != 2 {
		t.Fatalf("want 2 steps, got %d", len(r.JoinOrder))
	}
	// Last step: head vars only.
	last := r.JoinOrder[1].LiveVars
	gotLast := map[string]bool{}
	for _, v := range last {
		gotLast[v] = true
	}
	if !eqSet(gotLast, "x", "z") {
		t.Errorf("last LiveVars want {x,z}, got %v", gotLast)
	}
}

// TestProjectionPushdown_LiveVarsSorted: LiveVars must be sorted-deduped
// for plan equality and reproducibility.
func TestProjectionPushdown_LiveVarsSorted(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("R", "z", "a", "m"),
				posLit("A", "z", "a"),
				posLit("B", "a", "m"),
			),
		},
	}
	ep, errs := plan.Plan(prog, map[string]int{"A": 10, "B": 10})
	if len(errs) != 0 {
		t.Fatalf("plan errors: %v", errs)
	}
	for i, step := range ep.Strata[0].Rules[0].JoinOrder {
		for j := 1; j < len(step.LiveVars); j++ {
			if step.LiveVars[j-1] > step.LiveVars[j] {
				t.Errorf("step %d LiveVars not sorted: %v", i, step.LiveVars)
			}
			if step.LiveVars[j-1] == step.LiveVars[j] {
				t.Errorf("step %d LiveVars has duplicate: %v", i, step.LiveVars)
			}
		}
	}
}

// TestProjectionPushdown_RePlanStratumPreservesLiveVars: between-strata
// refresh must re-annotate LiveVars; otherwise stale plans would fall
// back to no-projection semantics.
func TestProjectionPushdown_RePlanStratumPreservesLiveVars(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			rule(atom("R", "x"),
				posLit("A", "x", "y"),
				posLit("B", "y", "z"),
			),
		},
	}
	ep, errs := plan.Plan(prog, map[string]int{"A": 100, "B": 100})
	if len(errs) != 0 {
		t.Fatalf("plan errors: %v", errs)
	}
	// Wipe LiveVars to simulate a stale plan, then re-plan.
	for i := range ep.Strata[0].Rules[0].JoinOrder {
		ep.Strata[0].Rules[0].JoinOrder[i].LiveVars = nil
	}
	plan.RePlanStratum(&ep.Strata[0], map[string]int{"A": 100, "B": 100})
	for i, step := range ep.Strata[0].Rules[0].JoinOrder {
		if step.LiveVars == nil {
			t.Errorf("step %d LiveVars nil after RePlanStratum", i)
		}
	}
}
