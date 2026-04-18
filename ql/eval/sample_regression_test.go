package eval_test

import (
	"context"
	"testing"
	"time"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// buildDisjShape returns the synthetic Mastodon `_disj_2 = 419k`-style
// regression program. The join product is intentionally scaled to
// EXCEED the binding cap used by the test (100_000): A has 1000 tuples
// (x, 1) and B has 1000 tuples (1, k), so Disj(x, z) :- A(x, y), B(y,
// z) materialises 1,000,000 tuples.
//
// This is the load-bearing scaling decision: the materialising pre-
// pass MUST hit the binding cap and bail (leaving no hint), so the
// only way `Disj` ends up with a hint ≥ 50_000 is if the sampling
// estimator is actually running and emitting it.
func buildDisjShape() (*datalog.Program, map[string]*eval.Relation, map[string]int) {
	A := eval.NewRelation("A", 2)
	B := eval.NewRelation("B", 2)
	for x := int64(0); x < 1000; x++ {
		A.Add(eval.Tuple{eval.IntVal{V: x}, eval.IntVal{V: 1}})
	}
	for k := int64(0); k < 1000; k++ {
		B.Add(eval.Tuple{eval.IntVal{V: 1}, eval.IntVal{V: k}})
	}
	Tiny := eval.NewRelation("Tiny", 1)
	Tiny.Add(eval.Tuple{eval.IntVal{V: 1}})

	disjRule := datalog.Rule{
		Head: datalog.Atom{Predicate: "Disj", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "z"}}},
		Body: []datalog.Literal{
			{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}}},
			{Positive: true, Atom: datalog.Atom{Predicate: "B", Args: []datalog.Term{datalog.Var{Name: "y"}, datalog.Var{Name: "z"}}}},
		},
	}
	consumerRule := datalog.Rule{
		Head: datalog.Atom{Predicate: "Consumer", Args: []datalog.Term{datalog.Var{Name: "x"}}},
		Body: []datalog.Literal{
			{Positive: true, Atom: datalog.Atom{Predicate: "Tiny", Args: []datalog.Term{datalog.Var{Name: "x"}}}},
			{Positive: true, Atom: datalog.Atom{Predicate: "Disj", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "z"}}}},
		},
	}
	prog := &datalog.Program{Rules: []datalog.Rule{disjRule, consumerRule}}
	base := map[string]*eval.Relation{"A": A, "B": B, "Tiny": Tiny}
	hints := map[string]int{"A": A.Len(), "B": B.Len(), "Tiny": Tiny.Len()}
	return prog, base, hints
}

// disjShapeBindingCap is the per-rule binding cap fed into
// EstimateAndPlan for the regression: comfortably below the 1M join
// product so the materialising path is guaranteed to bail.
const disjShapeBindingCap = 100_000

// TestP2bRegression_DisjShapeBoundedPlanTime mirrors the mastodon
// `_disj_2 = 419k` shape that motivated P2b. The join product
// (1_000_000) is deliberately ABOVE disjShapeBindingCap (100_000) so
// the materialising pre-pass cannot complete — only the sampling
// estimator can produce a hint.
//
// Asserts:
//  1. Pre-pass + plan time stays bounded.
//  2. `Disj` ends up with a sampled hint ≥ 50_000 (true=1_000_000).
//  3. The Consumer rule's planner output places Tiny first.
//
// The companion test below (sampling disabled) asserts that the SAME
// shape produces NO Disj hint, proving sampling is the load-bearing
// piece.
func TestP2bRegression_DisjShapeBoundedPlanTime(t *testing.T) {
	prog, base, hints := buildDisjShape()
	hook := eval.MakeEstimatorHook(base)

	start := time.Now()
	execPlan, planErrs := plan.EstimateAndPlan(prog, hints, disjShapeBindingCap, hook, plan.Plan)
	dur := time.Since(start)

	if len(planErrs) > 0 {
		t.Fatalf("plan errors: %v", planErrs)
	}
	if execPlan == nil {
		t.Fatal("nil execution plan")
	}
	if dur > 2*time.Second {
		t.Errorf("EstimateAndPlan took %s on _disj_2-shape regression — sampler may have regressed", dur)
	}
	t.Logf("EstimateAndPlan wall: %s", dur)

	// True join size is 1_000_000. Generous tolerance — sampling is a
	// noisy estimate but MUST be far above the 50_000 floor.
	disjHint, ok := hints["Disj"]
	if !ok {
		t.Fatalf("Disj hint missing — sampler did not run on the multi-rule disj shape (cap=%d)", disjShapeBindingCap)
	}
	if disjHint < 50_000 {
		t.Errorf("Disj sampled hint %d below expected ≥50_000 (true=1_000_000); sampler may not be reaching the consumer", disjHint)
	}

	var consumerPlanned *plan.PlannedRule
	for si := range execPlan.Strata {
		for ri := range execPlan.Strata[si].Rules {
			if execPlan.Strata[si].Rules[ri].Head.Predicate == "Consumer" {
				consumerPlanned = &execPlan.Strata[si].Rules[ri]
			}
		}
	}
	if consumerPlanned == nil {
		t.Fatal("Consumer rule not found in plan")
	}
	if len(consumerPlanned.JoinOrder) == 0 {
		t.Fatal("Consumer rule has empty JoinOrder")
	}
	first := consumerPlanned.JoinOrder[0].Literal
	if first.Atom.Predicate != "Tiny" {
		t.Errorf("Consumer first join step: got %q, want %q (planner did not honour sampled Disj hint)",
			first.Atom.Predicate, "Tiny")
	}
}

// TestP2bRegression_DisjShape_SamplingOff_BindingCapHits is the
// discriminator: with sampling disabled on the SAME shape, the
// materialising pre-pass MUST hit the binding cap and bail, leaving
// `Disj` unestimated. This proves the previous test's success was
// load-bearing on the sampling path, not an artefact of the shape
// fitting under the cap.
func TestP2bRegression_DisjShape_SamplingOff_BindingCapHits(t *testing.T) {
	prevEnabled := eval.SamplingEnabled
	eval.SamplingEnabled = false
	t.Cleanup(func() { eval.SamplingEnabled = prevEnabled })

	prog, base, hints := buildDisjShape()
	hook := eval.MakeEstimatorHook(base)

	execPlan, planErrs := plan.EstimateAndPlan(prog, hints, disjShapeBindingCap, hook, plan.Plan)
	if len(planErrs) > 0 {
		t.Fatalf("plan errors: %v", planErrs)
	}
	if execPlan == nil {
		t.Fatal("nil execution plan")
	}

	// Updated contract (PR for #145 catalog): with sampling off, the
	// materialising pre-pass tries to evaluate Disj fully, hits
	// disjShapeBindingCap (1M product vs 100k cap), and now records
	// the saturated-large hint instead of leaving Disj unestimated.
	// This is the load-bearing fix — the planner needs *some* signal
	// that Disj is huge, not the default 1000.
	//
	// The original discriminator (sampling off ⇒ no hint) is no
	// longer the right shape because both paths now produce a large
	// hint on cap-hit. The discriminator now is: with sampling off,
	// the hint must be *exactly* SaturatedSizeHint (not a sampled
	// estimate, which would be in the 500k–2M noise band).
	h, ok := hints["Disj"]
	if !ok {
		t.Fatalf("Disj hint missing with sampling OFF — cap-hit path should emit SaturatedSizeHint")
	}
	if h != eval.SaturatedSizeHint {
		t.Errorf("Disj hint = %d with sampling OFF — want SaturatedSizeHint=%d (cap-hit should saturate, not sample)",
			h, eval.SaturatedSizeHint)
	}
}

// TestP2bRegression_SamplingDisabledStillWorks: with sampling off,
// trivial single-rule IDBs that fit under the cap must still be
// materialised correctly. End-to-end smoke check that flipping the
// flag does not break evaluation on small inputs.
func TestP2bRegression_SamplingDisabledStillWorks(t *testing.T) {
	prevEnabled := eval.SamplingEnabled
	eval.SamplingEnabled = false
	t.Cleanup(func() { eval.SamplingEnabled = prevEnabled })

	A := eval.NewRelation("A", 1)
	for i := int64(0); i < 5; i++ {
		A.Add(eval.Tuple{eval.IntVal{V: i}})
	}
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "x"}}},
				Body: []datalog.Literal{{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}}}}},
			},
		},
	}
	base := map[string]*eval.Relation{"A": A}
	hints := map[string]int{"A": 5}
	hook := eval.MakeEstimatorHook(base)
	execPlan, errs := plan.EstimateAndPlan(prog, hints, 0, hook, plan.Plan)
	if len(errs) > 0 || execPlan == nil {
		t.Fatalf("plan failed with sampling off: errs=%v", errs)
	}
	if hints["Q"] != 5 {
		t.Errorf("Q hint with sampling off: want 5, got %d", hints["Q"])
	}

	rule := execPlan.Strata[0].Rules[0]
	tuples, err := eval.Rule(context.Background(), rule, eval.RelsOf(A), 0)
	if err != nil {
		t.Fatalf("Rule eval failed: %v", err)
	}
	if len(tuples) != 5 {
		t.Errorf("Q output: want 5 tuples, got %d", len(tuples))
	}
}

// TestCapHitHintDeprioritisesBigRule_PlannerIntegration: sampling OFF
// (so we exercise the materialising cap-hit path), big IDB +
// small base relation in a consumer rule. With the saturated-large
// hint correctly written, the planner must seed the consumer rule on
// the small base relation, not the big IDB.
//
// This is the planner integration test for the PR #145 catalog fix.
// Pre-fix: cap-hit left no hint → planner used default ~1000 → seeded
// the IDB → main eval blew the cap. Post-fix: cap-hit emits
// SaturatedSizeHint → planner sees IDB as huge → seeds the small
// base instead.
func TestCapHitHintDeprioritisesBigRule_PlannerIntegration(t *testing.T) {
	prevEnabled := eval.SamplingEnabled
	eval.SamplingEnabled = false
	t.Cleanup(func() { eval.SamplingEnabled = prevEnabled })

	prog, base, hints := buildDisjShape()
	hook := eval.MakeEstimatorHook(base)

	execPlan, planErrs := plan.EstimateAndPlan(prog, hints, disjShapeBindingCap, hook, plan.Plan)
	if len(planErrs) > 0 {
		t.Fatalf("plan errors: %v", planErrs)
	}
	if execPlan == nil {
		t.Fatal("nil execution plan")
	}

	// The cap-hit hint must be the saturated value, not default and
	// not absent.
	disjHint, ok := hints["Disj"]
	if !ok {
		t.Fatalf("Disj hint missing — cap-hit path should have emitted SaturatedSizeHint")
	}
	if disjHint != eval.SaturatedSizeHint {
		t.Fatalf("Disj hint = %d, want SaturatedSizeHint=%d", disjHint, eval.SaturatedSizeHint)
	}

	// Find the Consumer rule and confirm the planner picked Tiny (the
	// small base) as the seed, not Disj (the saturated-huge IDB).
	var consumerPlanned *plan.PlannedRule
	for si := range execPlan.Strata {
		for ri := range execPlan.Strata[si].Rules {
			if execPlan.Strata[si].Rules[ri].Head.Predicate == "Consumer" {
				consumerPlanned = &execPlan.Strata[si].Rules[ri]
			}
		}
	}
	if consumerPlanned == nil {
		t.Fatal("Consumer rule not found in plan")
	}
	if len(consumerPlanned.JoinOrder) == 0 {
		t.Fatal("Consumer rule has empty JoinOrder")
	}
	first := consumerPlanned.JoinOrder[0].Literal
	if first.Atom.Predicate != "Tiny" {
		t.Errorf("Consumer first join step: got %q, want %q (planner did not honour saturated cap-hit hint — would seed Disj and blow cap on main eval)",
			first.Atom.Predicate, "Tiny")
	}
}
