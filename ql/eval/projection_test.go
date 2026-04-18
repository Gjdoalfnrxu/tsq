package eval_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// makeChainProgram builds a wide chain join `R(x0, xN) :- A0(x0,x1),
// A1(x1,x2), ..., A{N-1}(x{N-1}, xN)`. Head only references the first
// and last vars — every intermediate var is "dead" after one hop.
func makeChainProgram(n int) *datalog.Program {
	body := make([]datalog.Literal, n)
	for i := 0; i < n; i++ {
		body[i] = datalog.Literal{
			Positive: true,
			Atom: datalog.Atom{
				Predicate: fmt.Sprintf("A%d", i),
				Args: []datalog.Term{
					datalog.Var{Name: fmt.Sprintf("x%d", i)},
					datalog.Var{Name: fmt.Sprintf("x%d", i+1)},
				},
			},
		}
	}
	return &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: datalog.Atom{
					Predicate: "R",
					Args: []datalog.Term{
						datalog.Var{Name: "x0"},
						datalog.Var{Name: fmt.Sprintf("x%d", n)},
					},
				},
				Body: body,
			},
		},
	}
}

// makeChainRelations builds N edge relations where each Ai connects k
// fan-out values: tuple (i, j) for j in 0..k-1. The total chain output
// has k^N tuples but the head projects only first/last so the materialised
// rule output is bounded.
func makeChainRelations(n, k int) map[string]*eval.Relation {
	rels := map[string]*eval.Relation{}
	for i := 0; i < n; i++ {
		r := eval.NewRelation(fmt.Sprintf("A%d", i), 2)
		for x := 0; x < k; x++ {
			for y := 0; y < k; y++ {
				r.Add(eval.Tuple{eval.IntVal{V: int64(x)}, eval.IntVal{V: int64(y)}})
			}
		}
		rels[fmt.Sprintf("A%d/2", i)] = r
	}
	return rels
}

// TestEval_ProjectionPushdown_ChainProducesCorrectResults: end-to-end
// equivalence — projection-on must produce the same results as
// projection-off.
func TestEval_ProjectionPushdown_ChainProducesCorrectResults(t *testing.T) {
	const n, k = 3, 4
	prog := makeChainProgram(n)
	rels := makeChainRelations(n, k)

	hints := map[string]int{}
	for i := 0; i < n; i++ {
		hints[fmt.Sprintf("A%d", i)] = k * k
	}
	ep, errs := plan.Plan(prog, hints)
	if len(errs) != 0 {
		t.Fatalf("plan: %v", errs)
	}

	// Verify LiveVars are populated.
	steps := ep.Strata[0].Rules[0].JoinOrder
	for i, s := range steps {
		if s.LiveVars == nil {
			t.Errorf("step %d LiveVars nil", i)
		}
	}

	tuplesOn, err := eval.Rule(context.Background(), ep.Strata[0].Rules[0], rels, 0)
	if err != nil {
		t.Fatalf("Rule (projection on): %v", err)
	}

	// Now wipe LiveVars and re-evaluate to confirm equivalence.
	r2 := ep.Strata[0].Rules[0]
	for i := range r2.JoinOrder {
		r2.JoinOrder[i].LiveVars = nil
	}
	tuplesOff, err := eval.Rule(context.Background(), r2, rels, 0)
	if err != nil {
		t.Fatalf("Rule (projection off): %v", err)
	}

	// Sets must be identical.
	if len(tuplesOn) != len(tuplesOff) {
		t.Fatalf("result count differs: on=%d off=%d", len(tuplesOn), len(tuplesOff))
	}
	seen := map[string]bool{}
	for _, tup := range tuplesOff {
		seen[fmt.Sprint(tup)] = true
	}
	for _, tup := range tuplesOn {
		if !seen[fmt.Sprint(tup)] {
			t.Errorf("tuple %v in projection-on but not projection-off", tup)
		}
	}
}

// TestEval_ProjectionPushdown_BindingMapShrinks asserts the load-bearing
// observable contract of P3b: after a mid-chain step, the binding map
// (per row) carries fewer keys than it would without projection.
//
// We instrument via a sentinel: build a chain such that at the join step
// we want to inspect, the head needs only 2 of 5 bound vars. We can't
// inspect mid-evaluation directly, but we can compare the LiveVars
// length to the cumulative-bound length at that step.
func TestEval_ProjectionPushdown_BindingMapShrinks(t *testing.T) {
	const n = 5
	prog := makeChainProgram(n)
	hints := map[string]int{}
	for i := 0; i < n; i++ {
		hints[fmt.Sprintf("A%d", i)] = 100
	}
	ep, errs := plan.Plan(prog, hints)
	if len(errs) != 0 {
		t.Fatalf("plan: %v", errs)
	}
	steps := ep.Strata[0].Rules[0].JoinOrder
	// Cumulative bound count after each step:
	// after A0: {x0,x1} = 2
	// after A1: {x0,x1,x2} = 3
	// after A2: {x0,x1,x2,x3} = 4
	// after A3: {x0..x4} = 5
	// after A4: {x0..x5} = 6
	// Head needs {x0, x5} only.
	// LiveVars after A2 should be {x0} (only x0 bound and survives downstream:
	// x1 dead; x2 needed by A3; x3 needed by A3; wait — x3 not yet bound at A2).
	// After A2, bound={x0,x1,x2,x3}. Demand for steps[3..]+head = {x3,x4,x5,x0}.
	// Intersect: {x0, x3}. So LiveVars at step 2 = {x0, x3} — shrinks 4→2.
	got := map[string]bool{}
	for _, v := range steps[2].LiveVars {
		got[v] = true
	}
	if len(got) != 2 || !got["x0"] || !got["x3"] {
		t.Errorf("step 2 LiveVars want {x0,x3}, got %v", got)
	}
	// Without projection, step 2 would carry 4 bound vars. With projection,
	// only 2 keys per binding map — a 50% shrink at this step alone.
}

// TestEval_ProjectionPushdown_PeakRowSizeReduction is the spec contract:
// on a wide chain with a narrow head, peak per-binding row size with
// projection on must be ≥2x smaller than with projection off.
//
// We instrument projectBindings via a package-level test hook to observe
// the actual runtime width of every output binding produced under the
// projection regime. This proves the *implementation* honours the plan
// (a buggy projectBindings that ignored keep, or a buggy planner that
// over-shrunk LiveVars and broke evaluation, would both surface here)
// rather than asserting a property of LiveVars talking to itself.
func TestEval_ProjectionPushdown_PeakRowSizeReduction(t *testing.T) {
	const n, k = 8, 3
	prog := makeChainProgram(n)
	hints := map[string]int{}
	for i := 0; i < n; i++ {
		hints[fmt.Sprintf("A%d", i)] = k * k
	}
	ep, errs := plan.Plan(prog, hints)
	if len(errs) != 0 {
		t.Fatalf("plan: %v", errs)
	}
	steps := ep.Strata[0].Rules[0].JoinOrder

	// Planner-side ceiling: max LiveVars across steps. The runtime peak
	// per-binding width must not exceed this.
	plannedPeak := 0
	for _, s := range steps {
		if len(s.LiveVars) > plannedPeak {
			plannedPeak = len(s.LiveVars)
		}
	}
	if plannedPeak == 0 {
		t.Fatal("LiveVars never populated by planner")
	}

	// Without projection: per-row width = cumulative bound count after the
	// last step = n + 1 (x0..xN). This is the baseline we shrink against.
	peakOff := n + 1

	// Install observer to capture the actual runtime peak per-binding width.
	observedPeak := 0
	prev := eval.GetProjectBindingsObserver()
	eval.SetProjectBindingsObserver(func(width int) {
		if width > observedPeak {
			observedPeak = width
		}
	})
	t.Cleanup(func() { eval.SetProjectBindingsObserver(prev) })

	rels := makeChainRelations(n, k)
	if _, err := eval.Rule(context.Background(), ep.Strata[0].Rules[0], rels, 0); err != nil {
		t.Fatalf("Rule: %v", err)
	}

	if observedPeak == 0 {
		t.Fatal("projectBindings never invoked — projection pushdown not active")
	}
	// Implementation honours the plan: every output binding fits within
	// the planner's LiveVars ceiling.
	if observedPeak > plannedPeak {
		t.Errorf("runtime per-binding width exceeds planner ceiling: observed=%d planned=%d",
			observedPeak, plannedPeak)
	}
	// Projection actually shrinks runtime bindings vs the unprojected baseline.
	if observedPeak >= peakOff {
		t.Errorf("projection failed to shrink bindings: observed=%d off-baseline=%d",
			observedPeak, peakOff)
	}
	ratio := float64(peakOff) / float64(observedPeak)
	if ratio < 2.0 {
		t.Errorf("peak row size reduction below 2x target: off=%d observed=%d ratio=%.2f",
			peakOff, observedPeak, ratio)
	}
	t.Logf("peak per-binding width: off=%d observed=%d planned=%d (%.2fx reduction)",
		peakOff, observedPeak, plannedPeak, ratio)
}

// TestEval_ProjectionPushdown_FilterFastPathSiblingIsolation exercises the
// zero-free-var positive-atom shared-binding-map fast path in applyPositive.
// When an atom has no free variables (e.g. B(x) with x already bound), the
// fast path appends the SAME map pointer to many output rows. projectBindings
// must allocate a fresh map per output to prevent mutation in one row from
// corrupting siblings.
//
// Body shape: R(x) :- A(x), B(x), C(x). A binds x; B and C are pure filters
// (no free vars at their step). With projection ON, projectBindings runs
// after each filter step on N rows that all share one source map. A
// regression to in-place mutation would cause sibling-row corruption,
// observable as wrong tuple counts vs the projection-off baseline.
func TestEval_ProjectionPushdown_FilterFastPathSiblingIsolation(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: datalog.Atom{
					Predicate: "R",
					Args:      []datalog.Term{datalog.Var{Name: "x"}},
				},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}}}},
					{Positive: true, Atom: datalog.Atom{Predicate: "B", Args: []datalog.Term{datalog.Var{Name: "x"}}}},
					{Positive: true, Atom: datalog.Atom{Predicate: "C", Args: []datalog.Term{datalog.Var{Name: "x"}}}},
				},
			},
		},
	}
	a := eval.NewRelation("A", 1)
	b := eval.NewRelation("B", 1)
	c := eval.NewRelation("C", 1)
	for i := 0; i < 10; i++ {
		a.Add(eval.Tuple{eval.IntVal{V: int64(i)}})
		b.Add(eval.Tuple{eval.IntVal{V: int64(i)}})
		c.Add(eval.Tuple{eval.IntVal{V: int64(i)}})
	}
	rels := map[string]*eval.Relation{"A/1": a, "B/1": b, "C/1": c}

	ep, errs := plan.Plan(prog, map[string]int{"A": 10, "B": 10, "C": 10})
	if len(errs) != 0 {
		t.Fatalf("plan: %v", errs)
	}

	// Projection ON.
	tuplesOn, err := eval.Rule(context.Background(), ep.Strata[0].Rules[0], rels, 0)
	if err != nil {
		t.Fatalf("Rule (projection on): %v", err)
	}

	// Projection OFF (wipe LiveVars).
	r2 := ep.Strata[0].Rules[0]
	for i := range r2.JoinOrder {
		r2.JoinOrder[i].LiveVars = nil
	}
	tuplesOff, err := eval.Rule(context.Background(), r2, rels, 0)
	if err != nil {
		t.Fatalf("Rule (projection off): %v", err)
	}

	if len(tuplesOn) == 0 {
		t.Fatal("projection on returned no tuples")
	}
	if len(tuplesOn) != len(tuplesOff) {
		t.Fatalf("filter-fast-path sibling corruption: on=%d off=%d", len(tuplesOn), len(tuplesOff))
	}
	seen := map[string]bool{}
	for _, tup := range tuplesOff {
		seen[fmt.Sprint(tup)] = true
	}
	for _, tup := range tuplesOn {
		if !seen[fmt.Sprint(tup)] {
			t.Errorf("tuple %v in projection-on but not projection-off — sibling-row corruption suspected", tup)
		}
	}
}

// BenchmarkEval_Chain8_ProjectionOn vs Off — the spec target: at least
// 2x reduction in peak intermediate row size for a synthetic wide-chain
// shape with a narrow head.
func BenchmarkEval_Chain8_ProjectionOn(b *testing.B) {
	benchChain(b, 8, 5, true)
}

func BenchmarkEval_Chain8_ProjectionOff(b *testing.B) {
	benchChain(b, 8, 5, false)
}

func benchChain(b *testing.B, n, k int, projOn bool) {
	prog := makeChainProgram(n)
	hints := map[string]int{}
	for i := 0; i < n; i++ {
		hints[fmt.Sprintf("A%d", i)] = k * k
	}
	ep, errs := plan.Plan(prog, hints)
	if len(errs) != 0 {
		b.Fatalf("plan: %v", errs)
	}
	rule := ep.Strata[0].Rules[0]
	if !projOn {
		// Strip LiveVars to simulate the pre-P3b world.
		for i := range rule.JoinOrder {
			rule.JoinOrder[i].LiveVars = nil
		}
	}
	rels := makeChainRelations(n, k)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := eval.Rule(context.Background(), rule, rels, 0)
		if err != nil {
			b.Fatalf("Rule: %v", err)
		}
	}
}
