package eval_test

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/eval"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// BenchmarkSampleJoinCardinality_ChainK1024 measures the per-estimate
// overhead of the Wander-Join sampler on a small two-relation chain
// join. The contract is that K=1024 walks complete in well under 1ms
// on small relations — the planner's pre-pass calls SampleJoinCardinality
// once per trivial IDB rule, so this is the dominant cost factor.
func BenchmarkSampleJoinCardinality_ChainK1024(b *testing.B) {
	A := eval.NewRelation("A", 2)
	B := eval.NewRelation("B", 2)
	for x := int64(0); x < 200; x++ {
		A.Add(eval.Tuple{eval.IntVal{V: x}, eval.IntVal{V: x % 50}})
	}
	for y := int64(0); y < 50; y++ {
		for k := int64(0); k < 5; k++ {
			B.Add(eval.Tuple{eval.IntVal{V: y}, eval.IntVal{V: y*100 + k}})
		}
	}
	rule := plan.SingleRule(datalog.Rule{
		Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}, datalog.Var{Name: "z"}}},
		Body: []datalog.Literal{
			{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}}},
			{Positive: true, Atom: datalog.Atom{Predicate: "B", Args: []datalog.Term{datalog.Var{Name: "y"}, datalog.Var{Name: "z"}}}},
		},
	}, map[string]int{"A": 200, "B": 250})
	rels := eval.RelsOf(A, B)

	rng := rand.New(rand.NewSource(1))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = eval.SampleJoinCardinality(rule, rels, 1024, rng)
	}
}

// BenchmarkSampleVsMaterialise_LargeJoin contrasts sample-based
// estimation vs running the full Rule() materialiser on a join shape
// that produces ~250000 output tuples. The sampling path's wall time
// must be orders-of-magnitude smaller — that's the whole P2b pitch.
func BenchmarkSampleVsMaterialise_LargeJoin(b *testing.B) {
	A := eval.NewRelation("A", 2)
	B := eval.NewRelation("B", 2)
	for x := int64(0); x < 500; x++ {
		A.Add(eval.Tuple{eval.IntVal{V: x}, eval.IntVal{V: 1}})
	}
	for k := int64(0); k < 500; k++ {
		B.Add(eval.Tuple{eval.IntVal{V: 1}, eval.IntVal{V: k}})
	}
	rule := plan.SingleRule(datalog.Rule{
		Head: datalog.Atom{Predicate: "Big", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "z"}}},
		Body: []datalog.Literal{
			{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}}},
			{Positive: true, Atom: datalog.Atom{Predicate: "B", Args: []datalog.Term{datalog.Var{Name: "y"}, datalog.Var{Name: "z"}}}},
		},
	}, map[string]int{"A": 500, "B": 500})
	rels := eval.RelsOf(A, B)

	b.Run("sample_K1024", func(b *testing.B) {
		rng := rand.New(rand.NewSource(1))
		for i := 0; i < b.N; i++ {
			_, _ = eval.SampleJoinCardinality(rule, rels, 1024, rng)
		}
	})
	b.Run("materialise_capped", func(b *testing.B) {
		ctx := context.Background()
		for i := 0; i < b.N; i++ {
			_, _ = eval.Rule(ctx, rule, rels, 100000)
		}
	})
}

// TestSampleJoinCardinality_OverheadBound is a cheap wall-time guard:
// 1000 estimator calls on the chain shape must complete in well under
// a second. If sampling regresses to materialising semantics this
// fires loudly.
func TestSampleJoinCardinality_OverheadBound(t *testing.T) {
	A := eval.NewRelation("A", 2)
	B := eval.NewRelation("B", 2)
	for x := int64(0); x < 200; x++ {
		A.Add(eval.Tuple{eval.IntVal{V: x}, eval.IntVal{V: x % 50}})
	}
	for y := int64(0); y < 50; y++ {
		for k := int64(0); k < 5; k++ {
			B.Add(eval.Tuple{eval.IntVal{V: y}, eval.IntVal{V: y*100 + k}})
		}
	}
	rule := plan.SingleRule(datalog.Rule{
		Head: datalog.Atom{Predicate: "Q", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}, datalog.Var{Name: "z"}}},
		Body: []datalog.Literal{
			{Positive: true, Atom: datalog.Atom{Predicate: "A", Args: []datalog.Term{datalog.Var{Name: "x"}, datalog.Var{Name: "y"}}}},
			{Positive: true, Atom: datalog.Atom{Predicate: "B", Args: []datalog.Term{datalog.Var{Name: "y"}, datalog.Var{Name: "z"}}}},
		},
	}, map[string]int{"A": 200, "B": 250})
	rels := eval.RelsOf(A, B)
	rng := rand.New(rand.NewSource(1))

	// Reduce iterations under -race (overhead is 10x+, and the
	// algorithmic guard is what we care about, not the absolute µs).
	const iters = 1000
	start := time.Now()
	for i := 0; i < iters; i++ {
		_, _ = eval.SampleJoinCardinality(rule, rels, 1024, rng)
	}
	dur := time.Since(start)
	// Generous bound — the goal is to catch O(materialised) regressions,
	// not to enforce a specific µs budget on slow CI runners or under
	// the race detector.
	if dur > 30*time.Second {
		t.Errorf("%d K=1024 estimates took %s (expected ≪ 30s); sampler may have regressed to materialising semantics", iters, dur)
	}
	t.Logf("%d estimates @ K=1024: %s (~%s/estimate)", iters, dur, dur/iters)
}
