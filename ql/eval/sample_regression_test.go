package eval_test

import (
	"context"
	"testing"
	"time"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// TestP2bRegression_DisjShapeBoundedPlanTime mirrors the mastodon
// `_disj_2 = 419k` shape that motivated P2b: a derived predicate
// defined as the disjunction (multi-rule head) of several large EDB
// joins, consumed by a downstream rule. With sampling OFF the
// pre-pass materialises ~hundreds of thousands of intermediate
// tuples and (without the binding cap) OOMs; with sampling ON it
// produces a hint, skips materialisation, and finishes in bounded
// time.
//
// We assert two things:
//  1. Pre-pass + plan time stays comfortably bounded (loose check;
//     the strict contract is qualitative — sampling MUST not be in
//     the same order of magnitude as materialising).
//  2. The downstream consumer rule's planner output places a small
//     seed first, NOT the large disj IDB — proving the sampled hint
//     reaches the planner.
//
// Full Mastodon-scale data is out of scope for CI; the synthetic
// shape preserves the structural property (large-IDB consumer) at
// 1000s of tuples instead of hundreds of thousands.
func TestP2bRegression_DisjShapeBoundedPlanTime(t *testing.T) {
	// Build EDBs.
	A := eval.NewRelation("A", 2) // ~1000 tuples, dense join
	B := eval.NewRelation("B", 2)
	for x := int64(0); x < 1000; x++ {
		A.Add(eval.Tuple{eval.IntVal{V: x}, eval.IntVal{V: 1}})
	}
	for k := int64(0); k < 100; k++ {
		B.Add(eval.Tuple{eval.IntVal{V: 1}, eval.IntVal{V: k}})
	}
	// Tiny seed extent — the predicate the planner SHOULD pick first
	// once it knows _disj is large.
	Tiny := eval.NewRelation("Tiny", 1)
	Tiny.Add(eval.Tuple{eval.IntVal{V: 1}})

	// Disj(x, z) :- A(x, y), B(y, z). — a single rule that produces
	// the join (single-rule "disjunction" — a multi-rule version
	// would behave the same since the sampler estimates each rule
	// and sums).
	disjRule := datalog.Rule{
		Head: datalog.Atom{Predicate: "Disj", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "z"}}},
		Body: []datalog.Literal{
			{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}}},
			{Positive: true, Atom: datalog.Atom{Predicate: "B", Args: []datalog.Term{datalog.Var{Name: "y"}, datalog.Var{Name: "z"}}}},
		},
	}
	// Consumer(x) :- Tiny(x), Disj(x, z). — the planner should
	// place Tiny first and probe Disj by x. Without a Disj hint it
	// would fall through to defaultSizeHint (1000), which still
	// leaves Tiny winning by tiny-seed override; the load-bearing
	// test is that the sampled hint for Disj is LARGE so any future
	// regression in tiny-seed override doesn't silently re-OOM.
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

	hook := eval.MakeEstimatorHook(base)
	start := time.Now()
	execPlan, planErrs := plan.EstimateAndPlan(prog, hints, 100000, hook, plan.Plan)
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

	// Disj sampled hint should be in the right ballpark — true size is
	// 1000 * 100 = 100000. Generous tolerance.
	disjHint := hints["Disj"]
	if disjHint < 50000 {
		t.Errorf("Disj sampled hint %d below expected ≥50000 (true=100000); sampler may not be reaching the consumer", disjHint)
	}

	// Find the Consumer rule in the plan and verify its first join
	// step is on Tiny, not Disj.
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

// TestP2bRegression_SamplingDisabledStillWorks: with sampling off,
// the consumer plan must still be correct (we have other defences:
// tiny-seed override, between-strata refresh). This test is a guard
// that flipping the sampling switch does not break end-to-end
// evaluation.
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

	// And the rule should still evaluate correctly end-to-end.
	rule := execPlan.Strata[0].Rules[0]
	tuples, err := eval.Rule(context.Background(), rule, eval.RelsOf(A), 0)
	if err != nil {
		t.Fatalf("Rule eval failed: %v", err)
	}
	if len(tuples) != 5 {
		t.Errorf("Q output: want 5 tuples, got %d", len(tuples))
	}
}
