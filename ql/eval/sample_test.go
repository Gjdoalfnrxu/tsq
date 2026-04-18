package eval_test

import (
	"math"
	"math/rand"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// makeRel2 builds an arity-2 base relation from int pairs (test helper).
// Lower-cased so it doesn't clash with the makeIntRel2 already in
// estimate_test.go but reads less awkwardly inline.
func makeRel2(name string, pairs [][2]int64) *eval.Relation {
	r := eval.NewRelation(name, 2)
	for _, p := range pairs {
		r.Add(eval.Tuple{eval.IntVal{V: p[0]}, eval.IntVal{V: p[1]}})
	}
	return r
}

// planRule helper: runs orderJoins via plan.SingleRule for a given body.
func planRule(head datalog.Atom, body []datalog.Literal, hints map[string]int) plan.PlannedRule {
	if hints == nil {
		hints = map[string]int{}
	}
	return plan.SingleRule(datalog.Rule{Head: head, Body: body}, hints)
}

// relMap canonicalises a flat list of relations into the (name,arity)
// keyed map shape SampleJoinCardinality expects.
func relMap(rels ...*eval.Relation) map[string]*eval.Relation {
	return eval.RelsOf(rels...)
}

// TestSampleJoinCardinality_SingleAtom: estimating a one-atom rule
// must return the relation's exact size (single random tuple, fanout
// = 1, weight = |R|; mean over K trials = |R|).
func TestSampleJoinCardinality_SingleAtom(t *testing.T) {
	A := eval.NewRelation("A", 1)
	for i := int64(0); i < 100; i++ {
		A.Add(eval.Tuple{eval.IntVal{V: i}})
	}
	rule := planRule(
		datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "x"}}},
		[]datalog.Literal{{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}}}}},
		map[string]int{"A": 100},
	)
	est, ok := eval.SampleJoinCardinality(rule, relMap(A), 256, rand.New(rand.NewSource(42)))
	if !ok {
		t.Fatal("expected sampling to succeed")
	}
	if est != 100 {
		t.Errorf("single-atom estimate: want 100 (exact), got %d", est)
	}
}

// TestSampleJoinCardinality_ChainJoinUnbiased: a chain join
// Q(x,y,z) :- A(x,y), B(y,z) where the true join size is known.
// Build relations so every A.y has exactly fanoutB matches in B,
// giving a true join size of |A| * fanoutB. The Wander-Join estimator
// must converge to this within tolerance.
func TestSampleJoinCardinality_ChainJoinUnbiased(t *testing.T) {
	const sizeA = 200
	const fanoutB = 5
	A := eval.NewRelation("A", 2)
	B := eval.NewRelation("B", 2)
	for x := int64(0); x < sizeA; x++ {
		// A(x, x%50) — 200 tuples, B keys repeat over 50 distinct values.
		A.Add(eval.Tuple{eval.IntVal{V: x}, eval.IntVal{V: x % 50}})
	}
	for y := int64(0); y < 50; y++ {
		for k := int64(0); k < fanoutB; k++ {
			B.Add(eval.Tuple{eval.IntVal{V: y}, eval.IntVal{V: y*100 + k}})
		}
	}
	trueSize := sizeA * fanoutB // = 1000

	rule := planRule(
		datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}, datalog.Var{Name: "z"}}},
		[]datalog.Literal{
			{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}}},
			{Positive: true, Atom: datalog.Atom{Predicate: "B", Args: []datalog.Term{datalog.Var{Name: "y"}, datalog.Var{Name: "z"}}}},
		},
		map[string]int{"A": sizeA, "B": int(B.Len())},
	)

	// Average across multiple seeds to characterise estimator bias.
	const runs = 20
	var sum float64
	for s := 0; s < runs; s++ {
		est, ok := eval.SampleJoinCardinality(rule, relMap(A, B), 1024, rand.New(rand.NewSource(int64(s+1))))
		if !ok {
			t.Fatalf("run %d: sampling failed", s)
		}
		sum += float64(est)
	}
	mean := sum / runs
	relErr := math.Abs(mean-float64(trueSize)) / float64(trueSize)
	if relErr > 0.10 {
		t.Errorf("chain-join mean estimate over %d runs: %.0f, true=%d, relErr=%.3f (>0.10)",
			runs, mean, trueSize, relErr)
	}
}

// TestSampleJoinCardinality_StarJoinUnbiased: star Q(x) :- A(x), B(x), C(x).
// True intersection = the shared x values. Verify the sampler approaches
// the true size on average.
func TestSampleJoinCardinality_StarJoinUnbiased(t *testing.T) {
	A := eval.NewRelation("A", 1)
	B := eval.NewRelation("B", 1)
	C := eval.NewRelation("C", 1)
	// A=[0..199], B=[100..299], C=[150..249]; intersection = [150..199] = 50.
	for i := int64(0); i < 200; i++ {
		A.Add(eval.Tuple{eval.IntVal{V: i}})
	}
	for i := int64(100); i < 300; i++ {
		B.Add(eval.Tuple{eval.IntVal{V: i}})
	}
	for i := int64(150); i < 250; i++ {
		C.Add(eval.Tuple{eval.IntVal{V: i}})
	}
	trueSize := 50

	rule := planRule(
		datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "x"}}},
		[]datalog.Literal{
			{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}}}},
			{Positive: true, Atom: datalog.Atom{Predicate: "B", Args: []datalog.Term{datalog.Var{Name: "x"}}}},
			{Positive: true, Atom: datalog.Atom{Predicate: "C", Args: []datalog.Term{datalog.Var{Name: "x"}}}},
		},
		map[string]int{"A": 200, "B": 200, "C": 100},
	)

	const runs = 20
	var sum float64
	for s := 0; s < runs; s++ {
		est, ok := eval.SampleJoinCardinality(rule, relMap(A, B, C), 1024, rand.New(rand.NewSource(int64(s+1))))
		if !ok {
			// Acceptable for a star-join with 25% selectivity under a
			// small K, but with K=1024 and 50/200=25% seed yield this
			// should not happen.
			t.Fatalf("run %d: sampling failed", s)
		}
		sum += float64(est)
	}
	mean := sum / runs
	relErr := math.Abs(mean-float64(trueSize)) / float64(trueSize)
	if relErr > 0.30 {
		t.Errorf("star-join mean over %d runs: %.0f, true=%d, relErr=%.3f (>0.30)",
			runs, mean, trueSize, relErr)
	}
}

// TestSampleJoinCardinality_SelectivePredicate: a join with a
// comparison filter. The estimator must factor the comparison's
// selectivity in by killing walks that fail the filter — naively
// counting all walk completions would over-count.
func TestSampleJoinCardinality_SelectivePredicate(t *testing.T) {
	A := eval.NewRelation("A", 1)
	for i := int64(0); i < 100; i++ {
		A.Add(eval.Tuple{eval.IntVal{V: i}})
	}
	// Q(x) :- A(x), x < 30. — true size = 30.
	rule := planRule(
		datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "x"}}},
		[]datalog.Literal{
			{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}}}},
			{Positive: true, Cmp: &datalog.Comparison{Op: "<", Left: datalog.Var{Name: "x"}, Right: datalog.IntConst{Value: 30}}},
		},
		map[string]int{"A": 100},
	)

	const runs = 20
	var sum float64
	for s := 0; s < runs; s++ {
		est, ok := eval.SampleJoinCardinality(rule, relMap(A), 1024, rand.New(rand.NewSource(int64(s+1))))
		if !ok {
			t.Fatalf("run %d: sampling failed", s)
		}
		sum += float64(est)
	}
	mean := sum / runs
	relErr := math.Abs(mean-30.0) / 30.0
	if relErr > 0.25 {
		t.Errorf("selective-pred mean over %d runs: mean=%.1f, true=30, relErr=%.3f",
			runs, mean, relErr)
	}
}

// TestSampleJoinCardinality_EmptySeedFails: when the planned seed
// relation is empty the sampler must return ok=false (not 0/true) so
// the caller can fall back to materialised counting.
func TestSampleJoinCardinality_EmptySeedFails(t *testing.T) {
	A := eval.NewRelation("A", 1) // empty
	rule := planRule(
		datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "x"}}},
		[]datalog.Literal{{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}}}}},
		map[string]int{"A": 0},
	)
	est, ok := eval.SampleJoinCardinality(rule, relMap(A), 256, rand.New(rand.NewSource(1)))
	if ok {
		t.Errorf("expected ok=false on empty seed, got est=%d ok=true", est)
	}
}

// TestSampleJoinCardinality_AggregateRejected: bodies containing an
// aggregate sub-goal cannot be sampled; the function returns
// ok=false so the materialising path takes over.
func TestSampleJoinCardinality_AggregateRejected(t *testing.T) {
	A := eval.NewRelation("A", 1)
	A.Add(eval.Tuple{eval.IntVal{V: 1}})
	rule := planRule(
		datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "n"}}},
		[]datalog.Literal{
			{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}}}},
			{Positive: true, Agg: &datalog.Aggregate{Func: "count", ResultVar: datalog.Var{Name: "n"}}},
		},
		map[string]int{"A": 1},
	)
	if _, ok := eval.SampleJoinCardinality(rule, relMap(A), 64, rand.New(rand.NewSource(1))); ok {
		t.Errorf("expected ok=false on aggregate body, got ok=true")
	}
}

// TestSampleJoinCardinality_NoMatchKillsWalk: a chain where the join
// key is disjoint between A and B. The sampler should report ok=false
// (zero successful walks).
func TestSampleJoinCardinality_NoMatchKillsWalk(t *testing.T) {
	A := makeRel2("A", [][2]int64{{1, 1}, {2, 2}, {3, 3}})
	B := makeRel2("B", [][2]int64{{99, 1}, {88, 2}}) // no y in {1,2,3}
	rule := planRule(
		datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "z"}}},
		[]datalog.Literal{
			{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}}},
			{Positive: true, Atom: datalog.Atom{Predicate: "B", Args: []datalog.Term{datalog.Var{Name: "y"}, datalog.Var{Name: "z"}}}},
		},
		map[string]int{"A": 3, "B": 2},
	)
	if est, ok := eval.SampleJoinCardinality(rule, relMap(A, B), 256, rand.New(rand.NewSource(1))); ok {
		t.Errorf("expected ok=false on disjoint-key join, got est=%d ok=true", est)
	}
}

// TestEstimateNonRecursiveIDBSizes_SamplingSkipsLargeMaterialisation:
// integration-level guard for the P2b OOM-avoidance contract. A
// trivial IDB whose true cardinality is well above
// SamplingMaterialiseThreshold gets a non-zero hint via sampling
// without the materialising path running. We can't directly observe
// "didn't materialise", but we can verify the hint is present and
// roughly correct, AND we can verify the function returns in bounded
// time even when the per-rule cap is 0 (which would let the
// materialising path go unbounded on this shape).
func TestEstimateNonRecursiveIDBSizes_SamplingSkipsLargeMaterialisation(t *testing.T) {
	// Build a join shape that would produce ~|A|*|B|/distinct(A.y) =
	// 1000*1000/100 = 10000 tuples. With SamplingMaterialiseThreshold
	// at 50000 this is below the gate, so to actually exercise the
	// skip path we need a bigger fanout. Use 500*500 dense join =
	// 250000 tuples.
	A := eval.NewRelation("A", 2)
	B := eval.NewRelation("B", 2)
	for x := int64(0); x < 500; x++ {
		A.Add(eval.Tuple{eval.IntVal{V: x}, eval.IntVal{V: 1}}) // all share y=1
	}
	for k := int64(0); k < 500; k++ {
		B.Add(eval.Tuple{eval.IntVal{V: 1}, eval.IntVal{V: k}}) // all share y=1
	}
	prog := &datalog.Program{
		Rules: []datalog.Rule{
			{
				Head: datalog.Atom{Predicate: "Big", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "z"}}},
				Body: []datalog.Literal{
					{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}}},
					{Positive: true, Atom: datalog.Atom{Predicate: "B", Args: []datalog.Term{datalog.Var{Name: "y"}, datalog.Var{Name: "z"}}}},
				},
			},
		},
	}
	base := map[string]*eval.Relation{
		"A": A, "B": B,
	}
	hints := map[string]int{"A": 500, "B": 500}
	updates := eval.EstimateNonRecursiveIDBSizes(prog, base, hints, 0)
	// True size is 250000. Sampling estimate should be in the right
	// order of magnitude — a generous tolerance is fine; the contract
	// here is "didn't fall back to default 1000 hint or zero".
	got := updates["Big"]
	if got < 50000 {
		t.Errorf("Big sampled hint: %d (expected > 50000, true=250000)", got)
	}
	if hints["Big"] != got {
		t.Errorf("hints[Big]=%d != updates[Big]=%d", hints["Big"], got)
	}
}

// TestEstimateNonRecursiveIDBSizes_SamplingDisabledMatchesLegacy:
// flipping SamplingEnabled=false reverts to the bit-identical
// materialising path. Defensive against a future change to the
// sampling default.
func TestEstimateNonRecursiveIDBSizes_SamplingDisabledMatchesLegacy(t *testing.T) {
	// Small case where materialising is fine and gives the exact
	// answer; sampling-disabled must produce that exact answer.
	A := eval.NewRelation("A", 1)
	for i := int64(0); i < 7; i++ {
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

	prevEnabled := eval.SamplingEnabled
	eval.SamplingEnabled = false
	t.Cleanup(func() { eval.SamplingEnabled = prevEnabled })

	base := map[string]*eval.Relation{"A": A}
	hints := map[string]int{"A": 7}
	updates := eval.EstimateNonRecursiveIDBSizes(prog, base, hints, 0)
	if updates["Q"] != 7 {
		t.Errorf("disabled-sampling Q size: want 7, got %d", updates["Q"])
	}
}
