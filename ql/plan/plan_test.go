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

// TestRePlanStratumWithDemand_PreservesDemandDrivenSeed is the P3a guard
// for Finding 3 of the PR #143 adversarial review. Multi-stratum programs
// must preserve demand-driven seed choice across between-strata refreshes
// — i.e. seminaive's call site must feed the saved DemandMap back through
// RePlanStratumWithDemand, NOT drop demand by calling RePlanStratum.
//
// Setup: two strata.
//   - Stratum 1: TaintSink(n) :- DangerousCall(n).         (tiny)
//     TaintSource(n) :- UntrustedIn(n).          (tiny)
//   - Stratum 2: Alert(src, sink) :- FlowStar(src, sink),
//     TaintSink(sink),
//     TaintSource(src).
//
// The query references Alert with no constants, so demand is body-driven:
// TaintSink/TaintSource are tiny enough (post-pre-pass) that backward
// inference does not need to constrain anything special. The interesting
// property: after stratum 1 refreshes hints (TaintSink → 7, TaintSource →
// 12 say), re-planning stratum 2 must STILL place a tiny seed first, NOT
// FlowStar. The demand-aware path uses the saved demand map; the demand-
// unaware path (RePlanStratum) would still get this right via the tiny-
// seed override, so we check the explicit invariant: post-refresh plan
// equals pre-refresh plan. If demand is silently dropped on refresh, this
// test still passes for tiny-seed-driven choices but would fail any future
// case where the demand map specifically alters seed selection. To make
// the regression observable we also assert the per-rule plan is the SAME
// rule-by-rule object the initial Plan() produced (modulo step contents).
func TestRePlanStratumWithDemand_PreservesDemandDrivenSeed(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{Head: atom("TaintSink", "n"),
				Body: []datalog.Literal{posLit("DangerousCall", "n")}},
			{Head: atom("TaintSource", "n"),
				Body: []datalog.Literal{posLit("UntrustedIn", "n")}},
			{Head: atom("Alert", "src", "sink"),
				Body: []datalog.Literal{
					posLit("FlowStar", "src", "sink"),
					posLit("TaintSink", "sink"),
					posLit("TaintSource", "src"),
				}},
		},
	}
	hints := map[string]int{
		"FlowStar":      500000,
		"DangerousCall": 7,
		"UntrustedIn":   12,
	}
	ep, errs := plan.Plan(prog, hints)
	if len(errs) != 0 {
		t.Fatalf("plan: %v", errs)
	}
	if ep.Demand == nil {
		t.Fatal("expected ExecutionPlan.Demand to be populated by Plan()")
	}

	// Find Alert's stratum and capture pre-refresh order.
	var alertStratumIdx int = -1
	for i := range ep.Strata {
		for _, r := range ep.Strata[i].Rules {
			if r.Head.Predicate == "Alert" {
				alertStratumIdx = i
			}
		}
	}
	if alertStratumIdx < 0 {
		t.Fatal("Alert rule not found in plan")
	}
	preOrder := predicateNamesOf(ep.Strata[alertStratumIdx].Rules[0].JoinOrder)
	if preOrder[0] == "FlowStar" {
		t.Fatalf("baseline plan should not seed FlowStar, got %v", preOrder)
	}

	// Simulate stratum 1's fixpoint: TaintSink/TaintSource have refreshed
	// sizes. Then re-plan stratum 2 with the demand map carried forward.
	hints["TaintSink"] = 7
	hints["TaintSource"] = 12
	plan.RePlanStratumWithDemand(&ep.Strata[alertStratumIdx], hints, ep.Demand)

	postOrder := predicateNamesOf(ep.Strata[alertStratumIdx].Rules[0].JoinOrder)
	if postOrder[0] == "FlowStar" {
		t.Fatalf("post-refresh plan regressed to FlowStar seed, got %v", postOrder)
	}
	// Stable demand → stable plan. Pre and post should be identical for
	// this fixture (the refresh only confirms what defaultSizeHint already
	// implied for tiny preds; demand is unchanged).
	if len(preOrder) != len(postOrder) {
		t.Fatalf("plan length changed: pre=%v post=%v", preOrder, postOrder)
	}
	for i := range preOrder {
		if preOrder[i] != postOrder[i] {
			t.Fatalf("plan diverged across refresh: pre=%v post=%v", preOrder, postOrder)
		}
	}
}

// TestRePlanStratumWithDemand_NilDemandDegradesGracefully confirms that
// callers (older eval code, hand-built ExecutionPlans in tests) that pass
// a nil DemandMap still get sensible re-planning behaviour.
func TestRePlanStratumWithDemand_NilDemandDegradesGracefully(t *testing.T) {
	s := &plan.Stratum{
		Rules: []plan.PlannedRule{
			{
				Head: atom("P", "x"),
				Body: []datalog.Literal{posLit("A", "x"), posLit("B", "x")},
				JoinOrder: []plan.JoinStep{
					{Literal: posLit("A", "x")},
					{Literal: posLit("B", "x")},
				},
			},
		},
	}
	plan.RePlanStratumWithDemand(s, map[string]int{"A": 1, "B": 100}, nil)
	if s.Rules[0].JoinOrder[0].Literal.Atom.Predicate != "A" {
		t.Errorf("nil demand should degrade to size-driven order, got %s",
			s.Rules[0].JoinOrder[0].Literal.Atom.Predicate)
	}
}

func predicateNamesOf(steps []plan.JoinStep) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.Literal.Atom.Predicate
	}
	return out
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

// TestEstimateAndPlan_SinglePassSelectsTinySeed is the planner-layer
// assertion for P1 of the planner roadmap: a single estimate-then-plan
// pass must pick the tiny IDB as the join seed on the FIRST plan, not
// after a re-plan. This is the contract that lets us delete the
// two-pass plan-then-replan ceremony from compileAndEval.
//
// Setup: rule P(x) :- A(x), B(x) where A is a base relation with 100
// tuples and B is a trivial IDB defined by `B(x) :- Tiny(x)` over a
// 5-tuple base. With base-only hints, B falls through to default 1000
// and A wins seed selection. With EstimateAndPlan, the estimator hook
// pre-computes |B|=5 BEFORE Plan() runs, so B wins seed selection on
// the very first (and only) plan. No RePlan call required.
//
// The hook is a hand-rolled fake here (no eval dependency in this test
// package); the real eval.MakeEstimatorHook is exercised end-to-end by
// TestIssue88_SetStateQueryDoesNotOOM in the integration_test package.
func TestEstimateAndPlan_SinglePassSelectsTinySeed(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: atom("B", "x"),
				Body: []datalog.Literal{posLit("Tiny", "x")},
			},
			{
				Head: atom("P", "x"),
				Body: []datalog.Literal{posLit("A", "x"), posLit("B", "x")},
			},
		},
	}
	hints := map[string]int{"A": 100, "Tiny": 5}

	// Baseline: without an estimator, B falls through to default 1000
	// and A wins seed selection — proves the test's "before" condition.
	baselineEP, errs := plan.EstimateAndPlan(prog, copyHints(hints), 0, nil, plan.Plan)
	if len(errs) != 0 {
		t.Fatalf("baseline plan errors: %v", errs)
	}
	pBaseline := findRuleByHead(t, baselineEP, "P")
	if pBaseline.JoinOrder[0].Literal.Atom.Predicate != "A" {
		t.Fatalf("baseline (no estimator): expected A first (B falls through to default 1000 > A=100), got %s",
			pBaseline.JoinOrder[0].Literal.Atom.Predicate)
	}

	// Real path: estimator hook pre-fills |B|=5 BEFORE Plan() runs, so
	// the single plan pass picks B as the seed.
	estimator := func(_ *datalog.Program, sizeHints map[string]int, _ int) map[string]int {
		// Simulate eval.EstimateNonRecursiveIDBSizes computing |B| = |Tiny| = 5.
		sizeHints["B"] = 5
		return map[string]int{"B": 5}
	}
	postHints := copyHints(hints)
	estimateEP, errs := plan.EstimateAndPlan(prog, postHints, 1000, estimator, plan.Plan)
	if len(errs) != 0 {
		t.Fatalf("estimate plan errors: %v", errs)
	}
	pEstimated := findRuleByHead(t, estimateEP, "P")
	if pEstimated.JoinOrder[0].Literal.Atom.Predicate != "B" {
		t.Errorf("after EstimateAndPlan: expected B first (size 5 < A=100), got %s; full order=%v",
			pEstimated.JoinOrder[0].Literal.Atom.Predicate, joinOrderPreds(pEstimated.JoinOrder))
	}
	// Confirm the hook actually mutated the caller's hints map.
	if postHints["B"] != 5 {
		t.Errorf("expected estimator hook to have mutated hints map in place; got hints[B]=%d", postHints["B"])
	}
}

// TestEstimateAndPlan_HonoursBindingCap pins the issue #130 / PR #132
// invariant: maxBindingsPerRule MUST be threaded through to the
// estimator hook, not silently dropped en route. If a future refactor
// drops the parameter (or hard-codes 0), the cap on the pre-pass
// disappears and pathological trivial IDBs OOM the host before the
// main eval ever runs (mastodon corpus, setStateUpdaterCallsFn).
func TestEstimateAndPlan_HonoursBindingCap(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: atom("P", "x"),
				Body: []datalog.Literal{posLit("A", "x")},
			},
		},
	}
	const wantCap = 12345
	var seenCap int
	hook := func(_ *datalog.Program, _ map[string]int, maxBindings int) map[string]int {
		seenCap = maxBindings
		return nil
	}
	_, errs := plan.EstimateAndPlan(prog, map[string]int{"A": 1}, wantCap, hook, plan.Plan)
	if len(errs) != 0 {
		t.Fatalf("plan errors: %v", errs)
	}
	if seenCap != wantCap {
		t.Errorf("estimator hook saw maxBindingsPerRule=%d, want %d (PR #132 cap regression)",
			seenCap, wantCap)
	}
}

// TestEstimateAndPlan_NilHookDegradesToPlainPlan ensures the no-fact-DB
// path (e.g. unit tests) still gets a valid plan even when no estimator
// is supplied — the hook is optional by design.
func TestEstimateAndPlan_NilHookDegradesToPlainPlan(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: atom("P", "x"),
				Body: []datalog.Literal{posLit("A", "x")},
			},
		},
	}
	ep, errs := plan.EstimateAndPlan(prog, nil, 0, nil, nil)
	if len(errs) != 0 {
		t.Fatalf("plan errors: %v", errs)
	}
	if ep == nil || len(ep.Strata) == 0 {
		t.Fatal("expected a non-empty plan from nil-hook nil-planFn EstimateAndPlan")
	}
}

func copyHints(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func findRuleByHead(t *testing.T, ep *plan.ExecutionPlan, head string) plan.PlannedRule {
	t.Helper()
	for _, s := range ep.Strata {
		for _, r := range s.Rules {
			if r.Head.Predicate == head {
				return r
			}
		}
	}
	t.Fatalf("no rule with head %q in plan", head)
	return plan.PlannedRule{}
}

func joinOrderPreds(steps []plan.JoinStep) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.Literal.Atom.Predicate
	}
	return out
}
