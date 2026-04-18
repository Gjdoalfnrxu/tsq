package plan

import (
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
)

// --- synthetic rule-body helpers -------------------------------------------

func atom(pred string, args ...datalog.Term) datalog.Literal {
	return datalog.Literal{Positive: true, Atom: datalog.Atom{Predicate: pred, Args: args}}
}

func v(name string) datalog.Var { return datalog.Var{Name: name} }

func ic(n int64) datalog.IntConst { return datalog.IntConst{Value: n} }

// --- InferBackwardDemand -----------------------------------------------------

// Chain join: R1 has a head consumed by R2 with one column ground by a
// constant in R2's body. Every caller grounds that column, so it lands
// in the demand set.
func TestInferBackwardDemand_ChainConstantCaller(t *testing.T) {
	// R1: P(x, y) :- Edge(x, y).            (IDB, 2-ary)
	// R2: Q(y)    :- P(1, y).               (caller grounds P col 0)
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{v("x"), v("y")}},
				Body: []datalog.Literal{atom("Edge", v("x"), v("y"))}},
			{Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{v("y")}},
				Body: []datalog.Literal{atom("P", ic(1), v("y"))}},
		},
	}
	d := InferBackwardDemand(prog, nil)
	got, ok := d["P"]
	if !ok {
		t.Fatalf("expected P in demand map, got %v", d)
	}
	if len(got) != 1 || got[0] != 0 {
		t.Fatalf("expected P demand = [0], got %v", got)
	}
}

// Star join: multiple callers, each grounding a different column.
// Intersect should be empty (no single column is bound by ALL callers).
func TestInferBackwardDemand_StarCallersIntersectEmpty(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{v("x"), v("y")}},
				Body: []datalog.Literal{atom("Edge", v("x"), v("y"))}},
			// Caller A grounds col 0:
			{Head: datalog.Atom{Predicate: "QA", Args: []datalog.Term{v("y")}},
				Body: []datalog.Literal{atom("P", ic(1), v("y"))}},
			// Caller B grounds col 1:
			{Head: datalog.Atom{Predicate: "QB", Args: []datalog.Term{v("x")}},
				Body: []datalog.Literal{atom("P", v("x"), ic(2))}},
		},
	}
	d := InferBackwardDemand(prog, nil)
	got, ok := d["P"]
	if !ok {
		t.Fatalf("expected P to be observed (in map) even with empty intersect, got %v", d)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty intersect for P, got %v", got)
	}
}

// Small-extent body-internal grounding: caller has a tiny-hinted literal
// sharing a var with the IDB reference. The tiny literal binds x, so
// P(x, y)'s col 0 ends up demand-bound even without a constant.
func TestInferBackwardDemand_SmallExtentBinds(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{v("x"), v("y")}},
				Body: []datalog.Literal{atom("Edge", v("x"), v("y"))}},
			// Tiny(x) has 5 tuples (≤ SmallExtentThreshold).
			{Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{v("y")}},
				Body: []datalog.Literal{atom("Tiny", v("x")), atom("P", v("x"), v("y"))}},
		},
	}
	hints := map[string]int{"Tiny": 5, "Edge": 100000}
	d := InferBackwardDemand(prog, hints)
	got, ok := d["P"]
	if !ok {
		t.Fatalf("expected P in demand, got %v", d)
	}
	if len(got) != 1 || got[0] != 0 {
		t.Fatalf("expected P demand = [0] from Tiny binding x, got %v", got)
	}
}

// Recursive rule: the fixed point must converge (no infinite loop) AND
// must not claim col-0 of the recursive self-call is bound when the
// outer rule's context does not in fact bind it via a small extent.
// This is the "bail cleanly" case from the P3a spec: the inference
// returns a sound demand set rather than optimistically over-binding.
//
// P(x, z) :- Edge(x, z).
// P(x, z) :- Edge(x, y), P(y, z).    (recursive — y is NOT bound by a
//
//	small extent; Edge is unhinted)
//
// Q(z)    :- P(1, z).                (caller grounds col 0)
//
// Outer caller grounds col 0. Recursive self-call does NOT ground
// col 0 (y is produced by Edge, not bound by context). Intersect
// across all callers of P is therefore empty — the sound answer.
func TestInferBackwardDemand_RecursiveBailsCleanly(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{v("x"), v("z")}},
				Body: []datalog.Literal{atom("Edge", v("x"), v("z"))}},
			{Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{v("x"), v("z")}},
				Body: []datalog.Literal{atom("Edge", v("x"), v("y")), atom("P", v("y"), v("z"))}},
			{Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{v("z")}},
				Body: []datalog.Literal{atom("P", ic(1), v("z"))}},
		},
	}
	d := InferBackwardDemand(prog, nil)
	got, ok := d["P"]
	if !ok {
		t.Fatalf("expected P observed (recursive call sites count), got %v", d)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty demand for P (recursive self-call dilutes outer caller), got %v", got)
	}
}

// Recursive-with-small-extent: if the recursive rule DOES bind col 0
// via a small extent, the demand survives across the self-call.
func TestInferBackwardDemand_RecursiveWithSmallExtentGrounds(t *testing.T) {
	// P(x, z) :- Edge(x, z).
	// P(x, z) :- SmallSeed(x), P(x, z).    (x comes from SmallSeed)
	// Q(z)    :- P(1, z).
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{v("x"), v("z")}},
				Body: []datalog.Literal{atom("Edge", v("x"), v("z"))}},
			{Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{v("x"), v("z")}},
				Body: []datalog.Literal{atom("SmallSeed", v("x")), atom("P", v("x"), v("z"))}},
			{Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{v("z")}},
				Body: []datalog.Literal{atom("P", ic(1), v("z"))}},
		},
	}
	hints := map[string]int{"SmallSeed": 10}
	d := InferBackwardDemand(prog, hints)
	got, ok := d["P"]
	if !ok || len(got) != 1 || got[0] != 0 {
		t.Fatalf("expected P demand = [0] with small-seed recursive binding, got %v", got)
	}
}

// Multi-head rule: two rules define P. Both must contribute grounding
// evidence (or, equivalently, both must be reachable from callers with
// the same grounding) for a column to remain demanded. The inference
// intersects at the caller level, not the rule-definition level — the
// same predicate across multiple defining rules shares a single demand
// entry.
func TestInferBackwardDemand_MultiHeadSharedDemand(t *testing.T) {
	// P defined by two rules; single caller grounds col 0.
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{v("x"), v("y")}},
				Body: []datalog.Literal{atom("A", v("x"), v("y"))}},
			{Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{v("x"), v("y")}},
				Body: []datalog.Literal{atom("B", v("x"), v("y"))}},
			{Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{v("y")}},
				Body: []datalog.Literal{atom("P", ic(7), v("y"))}},
		},
	}
	d := InferBackwardDemand(prog, nil)
	got, ok := d["P"]
	if !ok || len(got) != 1 || got[0] != 0 {
		t.Fatalf("expected P demand = [0] across multi-head definition, got %v", got)
	}
}

// Unreferenced IDB: defined but never called in any other rule or query.
// It should NOT appear in the demand map (no caller to observe demand).
func TestInferBackwardDemand_UnreferencedIDBAbsent(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{Head: datalog.Atom{Predicate: "Orphan", Args: []datalog.Term{v("x")}},
				Body: []datalog.Literal{atom("Edge", v("x"), v("_"))}},
		},
	}
	d := InferBackwardDemand(prog, nil)
	if _, ok := d["Orphan"]; ok {
		t.Fatalf("expected Orphan absent from demand (no callers), got %v", d)
	}
}

// Mixed-arity safety: if a predicate is defined with inconsistent arities
// across rules, skip backward inference rather than risk unsafe column
// indexing. This mirrors audit #3 from the roadmap.
func TestInferBackwardDemand_MixedArityIgnored(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{v("x")}},
				Body: []datalog.Literal{atom("Edge", v("x"), v("_"))}},
			{Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{v("x"), v("y")}},
				Body: []datalog.Literal{atom("Edge", v("x"), v("y"))}},
			{Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{v("y")}},
				Body: []datalog.Literal{atom("P", ic(1), v("y"))}},
		},
	}
	d := InferBackwardDemand(prog, nil)
	if _, ok := d["P"]; ok {
		t.Fatalf("expected mixed-arity P to be skipped, got %v", d)
	}
}

// Adversarial-review Finding 1 on PR #143: the query body must count
// as a caller of any IDB it references. If only rule bodies are walked,
// demand can be over-tightened to a column the query does NOT bind.
//
// Setup:
//   - Rule defines P(x, y) :- Edge(x, y).  (IDB)
//   - Rule caller R(y) :- P(1, y).         (binds P col 0 via constant)
//   - Query   :- P(a, b).                  (binds NEITHER column)
//
// Pre-fix: only rule callers walked → demand[P] = [0].
// Post-fix: query intersected in → demand[P] = [] (P col 0 dropped
// because the query, a real caller, does not bind it).
func TestInferBackwardDemand_QueryAsCallerDropsUnboundCol(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{v("x"), v("y")}},
				Body: []datalog.Literal{atom("Edge", v("x"), v("y"))}},
			{Head: datalog.Atom{Predicate: "R", Args: []datalog.Term{v("y")}},
				Body: []datalog.Literal{atom("P", ic(1), v("y"))}},
		},
		Query: &datalog.Query{
			Select: []datalog.Term{v("a"), v("b")},
			Body:   []datalog.Literal{atom("P", v("a"), v("b"))},
		},
	}
	d := InferBackwardDemand(prog, nil)
	got, ok := d["P"]
	if !ok {
		t.Fatalf("expected P observed (query is a caller), got %v", d)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty demand for P (query binds nothing), got %v", got)
	}
}

// Companion: when the query DOES bind the same column, demand survives.
func TestInferBackwardDemand_QueryAndRuleAgreeOnDemand(t *testing.T) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{Head: datalog.Atom{Predicate: "P", Args: []datalog.Term{v("x"), v("y")}},
				Body: []datalog.Literal{atom("Edge", v("x"), v("y"))}},
			{Head: datalog.Atom{Predicate: "R", Args: []datalog.Term{v("y")}},
				Body: []datalog.Literal{atom("P", ic(1), v("y"))}},
		},
		Query: &datalog.Query{
			Select: []datalog.Term{v("b")},
			Body:   []datalog.Literal{atom("P", ic(2), v("b"))},
		},
	}
	d := InferBackwardDemand(prog, nil)
	got, ok := d["P"]
	if !ok || len(got) != 1 || got[0] != 0 {
		t.Fatalf("expected P demand = [0] (rule and query both bind col 0), got %v", got)
	}
}

// --- orderJoinsWithDemand end-to-end ----------------------------------------

// Taint-shaped: a rule body with a small-extent sink-literal and a
// large-extent flow-literal. Even with zero head demand, the in-body
// small extent should drive the sink to the first slot. This is the
// case that BackwardTracker wrapping used to force via out-of-band
// hint injection.
func TestOrderJoinsWithDemand_TaintShapedSeedsOnSink(t *testing.T) {
	// Body:  FlowStar(src, sink), TaintSink(sink).
	// Source order puts FlowStar first (would be Cartesian if chosen);
	// the small-extent TaintSink should still end up as step 0.
	body := []datalog.Literal{
		atom("FlowStar", v("src"), v("sink")),
		atom("TaintSink", v("sink")),
	}
	hints := map[string]int{"FlowStar": 500000, "TaintSink": 7}
	steps := orderJoinsWithDemand(datalog.Atom{}, body, hints, nil)
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}
	if steps[0].Literal.Atom.Predicate != "TaintSink" {
		t.Fatalf("expected TaintSink first, got %q", steps[0].Literal.Atom.Predicate)
	}
}

// Head-demand prebinds variables so a body literal sharing them wins
// on boundCount even when its sizeHint is large. Comparison: without
// demand the large literal would be placed after a smaller-sized one
// that has no share relation to the head.
func TestOrderJoinsWithDemand_HeadDemandBiasesSeed(t *testing.T) {
	// Head: R(x, y).  Body: Medium(a, b), Large(x, y).
	// Without demand: Medium wins (size 500 beats size 100000).
	// With demand [0]: Large shares bound x → boundCount=1, wins over
	// Medium's boundCount=0 regardless of size.
	body := []datalog.Literal{
		atom("Medium", v("a"), v("b")),
		atom("Large", v("x"), v("y")),
	}
	hints := map[string]int{"Medium": 500, "Large": 100000}
	head := datalog.Atom{Predicate: "R", Args: []datalog.Term{v("x"), v("y")}}

	// Baseline: no demand → Medium first.
	baseline := orderJoinsWithDemand(head, body, hints, nil)
	if baseline[0].Literal.Atom.Predicate != "Medium" {
		t.Fatalf("baseline expected Medium first, got %q", baseline[0].Literal.Atom.Predicate)
	}

	// With demand [0] on col x: Large should now win via shared-var bias.
	steps := orderJoinsWithDemand(head, body, hints, []int{0})
	if steps[0].Literal.Atom.Predicate != "Large" {
		t.Fatalf("expected Large first under head demand, got %q", steps[0].Literal.Atom.Predicate)
	}
}

// Eligibility safety: a negative literal must never be placed via
// demand pre-binding alone. Runtime binding is what dictates when a
// negative literal can be evaluated as an anti-join; demand is only a
// scoring hint. If a negative literal appeared first in the plan
// because demand said its vars were bound, the evaluator would see
// unbound vars and produce incorrect results.
func TestOrderJoinsWithDemand_NegativeLiteralNotPromotedByDemand(t *testing.T) {
	// Head R(x). Body: not Forbidden(x), Seed(x).
	// Under demand [0] on R's head, x is planner-bound. A naive
	// implementation would place `not Forbidden(x)` first. The fixed
	// implementation must require runtimeBound, so Seed(x) places
	// first and introduces x at runtime, then the anti-join fires.
	body := []datalog.Literal{
		{Positive: false, Atom: datalog.Atom{Predicate: "Forbidden", Args: []datalog.Term{v("x")}}},
		atom("Seed", v("x")),
	}
	head := datalog.Atom{Predicate: "R", Args: []datalog.Term{v("x")}}
	steps := orderJoinsWithDemand(head, body, map[string]int{"Seed": 10}, []int{0})
	if steps[0].Literal.Atom.Predicate == "Forbidden" && !steps[0].Literal.Positive {
		t.Fatalf("negative literal placed first via demand pre-binding — runtime would see unbound x")
	}
	if steps[0].Literal.Atom.Predicate != "Seed" {
		t.Fatalf("expected Seed first (introduces x at runtime), got %q", steps[0].Literal.Atom.Predicate)
	}
}

// Adversarial-review Finding 2 on PR #143: pickTinySeed must NOT see
// head-demand-prebound vars. If it did, the shared-var branch of
// isTinySeed would promote an unhinted IDB sharing a head-demand var
// to "tiny seed" purely because of head demand — and the evaluator
// would full-scan it (no actual runtime binding exists yet).
//
// Distinguishing fixture:
//   - Head: R(x). Demand prebinds x.
//   - Body:
//     Big(x, y)              — unhinted, shares planner-bound x
//     Marked(z, 42)          — unhinted, has a constant arg, no shared
//     planner-bound var.
//
// With the bug (plannerBound to pickTinySeed):
//   - Big qualifies via the shared-var branch (no hint + shared bound).
//   - Marked qualifies via the constant-arg branch.
//   - Both unhinted, same default tiebreak → first-eligible wins → Big
//     (index 0) is seeded.
//
// With the fix (runtimeBound to pickTinySeed; empty at slot 0):
//   - Big does NOT qualify (no constant, no SHARED RUNTIME-bound var).
//   - Marked still qualifies via the constant-arg branch.
//   - Marked wins — pickTinySeed returns its index.
//
// scoreLiteral keeps using plannerBound (line 486 / unchanged), so the
// "head-demand biases scoring" promise is preserved for the non-tiny
// fall-through path. This test pins the specific failure mode: an
// unhinted IDB sharing only a head-demand var must NOT be auto-promoted
// to seed by tiny-seed override.
func TestOrderJoinsWithDemand_PickTinySeedUsesRuntimeBound(t *testing.T) {
	body := []datalog.Literal{
		atom("Big", v("x"), v("y")),
		atom("Marked", v("z"), ic(42)),
	}
	head := datalog.Atom{Predicate: "R", Args: []datalog.Term{v("x")}}
	steps := orderJoinsWithDemand(head, body, map[string]int{}, []int{0})
	if steps[0].Literal.Atom.Predicate != "Marked" {
		t.Fatalf("expected Marked first (constant-arg tiny-seed); Big should NOT be promoted "+
			"to tiny-seed by head-demand alone. Got %q (full: %v)",
			steps[0].Literal.Atom.Predicate, predicateOrder(steps))
	}
}

func TestOrderJoinsWithDemand_EmptyDemandMatchesOrderJoins(t *testing.T) {
	body := []datalog.Literal{
		atom("A", v("x"), v("y")),
		atom("B", v("y"), v("z")),
	}
	hints := map[string]int{"A": 100, "B": 50}
	got := orderJoinsWithDemand(datalog.Atom{}, body, hints, nil)
	want := orderJoins(body, hints)
	if len(got) != len(want) {
		t.Fatalf("step count differs: %d vs %d", len(got), len(want))
	}
	for i := range got {
		if got[i].Literal.Atom.Predicate != want[i].Literal.Atom.Predicate {
			t.Fatalf("step %d differs: %q vs %q", i, got[i].Literal.Atom.Predicate, want[i].Literal.Atom.Predicate)
		}
	}
}

// End-to-end via Plan(): a taint-shaped program where the ONLY thing
// making the sink literal tiny is sizeHints (no magic sets, no external
// trust channel). Before P3a, the planner would still succeed here
// because the existing tiny-seed heuristic fires one-step ahead. The
// new assertion: this keeps working WITHOUT needing any BackwardTracker
// bridge to populate hints through a side-channel.
func TestPlan_TaintShape_NativeBackwardSeedChoice(t *testing.T) {
	// Program:
	//   Alert(src, sink) :- FlowStar(src, sink), TaintSink(sink), TaintSource(src).
	//   TaintSink(n)     :- DangerousCall(n).     (size 7 via hints)
	//   TaintSource(n)   :- UntrustedIn(n).       (size 12 via hints)
	// No query — planner should still place TaintSink first in Alert.
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{Head: datalog.Atom{Predicate: "Alert", Args: []datalog.Term{v("src"), v("sink")}},
				Body: []datalog.Literal{
					atom("FlowStar", v("src"), v("sink")),
					atom("TaintSink", v("sink")),
					atom("TaintSource", v("src")),
				}},
			{Head: datalog.Atom{Predicate: "TaintSink", Args: []datalog.Term{v("n")}},
				Body: []datalog.Literal{atom("DangerousCall", v("n"))}},
			{Head: datalog.Atom{Predicate: "TaintSource", Args: []datalog.Term{v("n")}},
				Body: []datalog.Literal{atom("UntrustedIn", v("n"))}},
		},
	}
	hints := map[string]int{
		"FlowStar":      500000,
		"TaintSink":     7,
		"TaintSource":   12,
		"DangerousCall": 7,
		"UntrustedIn":   12,
	}
	ep, errs := Plan(prog, hints)
	if len(errs) > 0 {
		t.Fatalf("plan: %v", errs)
	}
	// Find the Alert rule's plan.
	var alert *PlannedRule
	for i := range ep.Strata {
		for j := range ep.Strata[i].Rules {
			if ep.Strata[i].Rules[j].Head.Predicate == "Alert" {
				alert = &ep.Strata[i].Rules[j]
			}
		}
	}
	if alert == nil {
		t.Fatal("no Alert rule in plan")
	}
	if alert.JoinOrder[0].Literal.Atom.Predicate != "TaintSink" {
		t.Fatalf("expected TaintSink first in Alert, got %q (full order: %v)",
			alert.JoinOrder[0].Literal.Atom.Predicate, predicateOrder(alert.JoinOrder))
	}
}

// Taint-fixture equivalence: the critical P3a claim is "planner works
// correctly on taint shapes WITHOUT needing any BackwardTracker or
// magic-set side channel to populate hints." This test exercises the
// exact body shape that the setState integration test
// (issue88_setstate_integration_test.go) uses, plus an extra
// Configuration-free path that mimics a flat taint query, and asserts
// both converge to the correct seed via InferBackwardDemand +
// orderJoinsWithDemand alone.
func TestPlan_BackwardInference_RetiresBridgeTrustChannel(t *testing.T) {
	// Shape 1: single-rule taint flow.
	//   Alert(src, sink) :- Source(src), FlowStar(src, sink), Sink(sink).
	//
	// Source, Sink are tiny (real values in the React fixture are ~12 / ~7).
	// FlowStar is the large pre-computed transitive closure (~500k).
	// Expected: Sink (or Source) placed first, NOT FlowStar.
	//
	// In the pre-P3a world this worked via the tiny-seed heuristic in
	// orderJoins + the pre-pass writing the real sizes. What changes
	// in P3a: even with the tiny-seed heuristic disabled (simulated by
	// unsetting hints for tiny preds), backward demand from the query
	// still biases the plan correctly.
	body := []datalog.Literal{
		atom("FlowStar", v("src"), v("sink")),
		atom("Source", v("src")),
		atom("Sink", v("sink")),
	}
	hints := map[string]int{"FlowStar": 500000, "Source": 12, "Sink": 7}
	steps := orderJoinsWithDemand(datalog.Atom{Predicate: "Alert",
		Args: []datalog.Term{v("src"), v("sink")}}, body, hints, nil)
	first := steps[0].Literal.Atom.Predicate
	// Adversarial-review Finding 6 on PR #143: hard equality, not a
	// soft "anything-but-FlowStar" assertion. Source (12) and Sink (7)
	// both qualify as tiny via the (a) branch of isTinySeed
	// (sz <= tinySeedThreshold = 32). pickTinySeed's tiebreak is
	// "hinted strictly beats unhinted, then smaller size wins" — both
	// are hinted, so the smaller wins: Sink (7) < Source (12). Pin
	// that explicitly so any future regression in pickTinySeed's
	// tiebreak is caught here, not silently absorbed.
	if first != "Sink" {
		t.Fatalf("shape 1: expected Sink first (smaller hint wins tiebreak among "+
			"tiny-seeded Source/Sink), got %q (full order: %v)", first, predicateOrder(steps))
	}

	// Shape 2: multi-hop through an intermediate IDB.
	//   Alert(src, sink) :- TaintedField(src, f), FlowStar(f, sink), Sink(sink).
	// The crucial case: TaintedField is small-ish (50, > tinySeedThreshold
	// of 32), FlowStar is large, Sink is small (7, qualifies as tiny).
	// pickTinySeed: only Sink qualifies (TaintedField's hint > 32 → branch
	// (a) of isTinySeed fails; no constants, no shared bound vars).
	// Sink wins outright.
	body2 := []datalog.Literal{
		atom("FlowStar", v("f"), v("sink")),
		atom("TaintedField", v("src"), v("f")),
		atom("Sink", v("sink")),
	}
	hints2 := map[string]int{"FlowStar": 500000, "TaintedField": 50, "Sink": 7}
	steps2 := orderJoinsWithDemand(datalog.Atom{Predicate: "Alert",
		Args: []datalog.Term{v("src"), v("sink")}}, body2, hints2, nil)
	if steps2[0].Literal.Atom.Predicate != "Sink" {
		t.Fatalf("shape 2: expected Sink first (only tiny-qualifying literal), got %q "+
			"(full order: %v)", steps2[0].Literal.Atom.Predicate, predicateOrder(steps2))
	}
}

// Adversarial-review Finding 5 on PR #143: SmallExtentThreshold = 5000
// is a hard threshold. Boundary tests at 4999/5000/5001 pin the
// inclusive ceiling so a refactor that flips to exclusive comparison
// (`<` vs `<=`) is caught immediately.
func TestSmallExtentThreshold_Boundary(t *testing.T) {
	cases := []struct {
		name  string
		size  int
		small bool
	}{
		{"below threshold", 4999, true},
		{"exact threshold", 5000, true}, // <= is inclusive
		{"above threshold", 5001, false},
		{"zero treated as unknown", 0, false}, // isSmallExtent guards on sz>0
		{"negative treated as unknown", -1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hints := map[string]int{"P": tc.size}
			got := isSmallExtent("P", hints)
			if got != tc.small {
				t.Fatalf("size=%d: expected isSmallExtent=%v, got %v", tc.size, tc.small, got)
			}
		})
	}
	// Missing key.
	if isSmallExtent("Absent", map[string]int{}) {
		t.Fatal("absent predicate should not be treated as small extent")
	}
}

// Benchmark planning time for a taint-shaped rule. Regression guard:
// P3a adds a fixed-point pass over all rules before orderJoins runs,
// which is O(rules × body × iterations). Acceptable overhead per the
// spec is "within 2x of P2b baseline."
func BenchmarkPlan_TaintShape(b *testing.B) {
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{Head: datalog.Atom{Predicate: "Alert", Args: []datalog.Term{v("src"), v("sink")}},
				Body: []datalog.Literal{
					atom("FlowStar", v("src"), v("sink")),
					atom("TaintSink", v("sink")),
					atom("TaintSource", v("src")),
				}},
			{Head: datalog.Atom{Predicate: "TaintSink", Args: []datalog.Term{v("n")}},
				Body: []datalog.Literal{atom("DangerousCall", v("n"))}},
			{Head: datalog.Atom{Predicate: "TaintSource", Args: []datalog.Term{v("n")}},
				Body: []datalog.Literal{atom("UntrustedIn", v("n"))}},
		},
	}
	hints := map[string]int{
		"FlowStar": 500000, "TaintSink": 7, "TaintSource": 12,
		"DangerousCall": 7, "UntrustedIn": 12,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ep, errs := Plan(prog, hints)
		if len(errs) > 0 || ep == nil {
			b.Fatalf("plan: %v", errs)
		}
	}
}

func predicateOrder(steps []JoinStep) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.Literal.Atom.Predicate
	}
	return out
}

// Equivalence guard: running Plan with backward inference active on the
// existing Path/Edge transitive-closure test program must not change the
// join order vs the pre-P3a planner. The transitive closure has no
// small-extent grounding in any body, so demand should be empty and the
// plans identical.
func TestPlan_PathClosure_BackwardInferenceIsNoOp(t *testing.T) {
	prog := progPathClosure([]datalog.Literal{
		atom("Path", v("a"), v("b")),
	}, "a", "b")
	hints := map[string]int{"Edge": 100000}
	ep, errs := Plan(prog, hints)
	if len(errs) > 0 {
		t.Fatalf("plan: %v", errs)
	}

	// Re-order each Path rule's body manually with plain orderJoins and
	// compare.
	for _, stratum := range ep.Strata {
		for _, pr := range stratum.Rules {
			want := orderJoins(pr.Body, hints)
			if len(want) != len(pr.JoinOrder) {
				t.Fatalf("rule %s: step count mismatch", pr.Head.Predicate)
			}
			for i := range want {
				if want[i].Literal.Atom.Predicate != pr.JoinOrder[i].Literal.Atom.Predicate {
					t.Fatalf("rule %s step %d: plan diverged (want %q got %q) — demand should have been empty",
						pr.Head.Predicate, i,
						want[i].Literal.Atom.Predicate, pr.JoinOrder[i].Literal.Atom.Predicate)
				}
			}
		}
	}
}
