package eval

import (
	"math/rand"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// DefaultSampleK is the default number of sample walks per Wander-Join
// estimate. K=1024 is a balance between accuracy and bounded cost: at
// K=1024 with a 10-step join, a single estimate touches at most ~1e4
// tuples (~1ms wall on small relations), and the unbiased estimator's
// relative standard error is sqrt(1/K) ≈ 3% for chains of "well-mixing"
// joins (typically rises to 10-30% on skewed real workloads — see
// Li et al., SIGMOD 2016, Wander-Join section 4).
const DefaultSampleK = 1024

// SampleJoinCardinality estimates the output cardinality of a single
// PlannedRule by drawing K independent random walks across the join
// graph (the Wander-Join family of estimators; Li, Wu, Yi, SIGMOD 2016).
//
// Algorithm (one walk):
//
//  1. Pick the first positive atom in JoinOrder. Its relation is the
//     "seed" R₁ with size |R₁|. Choose a tuple uniformly at random.
//     Sampling probability for that seed = 1/|R₁|.
//
//  2. For each subsequent join step, given the current bindings:
//     - comparison: evaluate; on false the walk dies (X=0).
//     - negative literal: if any matching tuple exists, walk dies.
//     - positive atom: enumerate matching tuples via the relation's
//     HashIndex on bound columns. Let m be the count. If m=0 walk
//     dies. Else pick one uniformly at random; multiply the walk's
//     running fanout by m, extend bindings.
//
//  3. If the walk reaches the end of JoinOrder, it represents a real
//     output tuple; its inverse-probability weight is
//     |R₁| * Π m_i. The unbiased estimator is
//
//     Ŷ = (1/K) * Σ_(walks that survived) |R₁| * Π m_i
//
// Returns (estimate, true) on success. Returns (0, false) when:
//   - JoinOrder is empty,
//   - there is no positive-atom seed in the plan,
//   - the seed relation is missing or empty,
//   - the rule body contains an aggregate (not supported here — fall
//     back to the materialising path),
//   - K successive walks all die at the seed (zero successful samples).
//
// Returning ok=false signals "fall back to a materialising counter" to
// the caller; never silently return a misleading 0.
//
// Cost contract: at most K × len(JoinOrder) HashIndex lookups +
// len(JoinOrder) free-var binding extensions per call. No intermediate
// binding accumulation across walks (each walk reuses one scratch
// binding). See BenchmarkSampleJoinCardinality_* for the numbers.
//
// rng is the source of randomness. Pass a deterministic *rand.Rand for
// tests; pass a fresh rand.New(rand.NewSource(time.Now().UnixNano()))
// in production. nil is treated as a fresh, time-seeded rng (NOT the
// global rand source — we do not want the estimator to perturb global
// state).
func SampleJoinCardinality(
	rule plan.PlannedRule,
	rels map[string]*Relation,
	k int,
	rng *rand.Rand,
) (int, bool) {
	if k <= 0 {
		k = DefaultSampleK
	}
	if rng == nil {
		// Deterministic-by-default: a fresh, fixed-seed rng makes the
		// estimator reproducible in tests. Real callers (the planner
		// pre-pass) pass their own seeded source so production runs are
		// not bit-identical across invocations.
		rng = rand.New(rand.NewSource(1))
	}
	steps := rule.JoinOrder
	if len(steps) == 0 {
		return 0, false
	}

	// Find the seed: the first positive non-builtin atom step. Anything
	// before it (a comparison with no free vars, or an aggregate) cannot
	// be a seed by definition. If the very first such step is missing
	// the relation, we cannot sample.
	seedIdx := -1
	for i, step := range steps {
		if step.Literal.Cmp != nil {
			continue
		}
		if step.Literal.Agg != nil {
			// Aggregate sub-goals are evaluated post-fixpoint and do
			// not have a sampleable extent here. Bail out.
			return 0, false
		}
		if !step.Literal.Positive {
			// A leading negative literal would have no bindings to
			// anti-join against — the planner shouldn't emit this, but
			// if it does we cannot seed.
			continue
		}
		if IsBuiltin(step.Literal.Atom.Predicate) {
			// Builtins are procedural; no extent to draw from.
			continue
		}
		seedIdx = i
		break
	}
	if seedIdx == -1 {
		return 0, false
	}

	seedAtom := steps[seedIdx].Literal.Atom
	seedRel, ok := rels[relKey(seedAtom.Predicate, len(seedAtom.Args))]
	if !ok || seedRel == nil || seedRel.Len() == 0 {
		return 0, false
	}
	seedSize := seedRel.Len()

	// Reject any rule body containing an aggregate at all — they require
	// the materialising path. Already covered by the seed scan above for
	// leading aggregates; this catches mid-body cases.
	for _, step := range steps {
		if step.Literal.Agg != nil {
			return 0, false
		}
	}

	var sumWeights float64
	successful := 0

	for trial := 0; trial < k; trial++ {
		// Draw one tuple uniformly from the seed relation.
		seedTupleIdx := rng.Intn(seedSize)
		walkBinding := make(binding)
		if !extendWithTuple(walkBinding, seedAtom, seedRel.Tuples()[seedTupleIdx]) {
			// Wildcard or repeated-var inconsistency — extremely rare
			// for a well-formed seed. Walk dies.
			continue
		}

		fanout := 1.0
		alive := true
		for i, step := range steps {
			if i == seedIdx || !alive {
				continue
			}
			lit := step.Literal

			if lit.Cmp != nil {
				if !sampleEvalCmp(lit.Cmp, walkBinding) {
					alive = false
				}
				continue
			}
			if lit.Agg != nil {
				// Already filtered above; defensive.
				alive = false
				continue
			}
			if IsBuiltin(lit.Atom.Predicate) {
				// Builtins on a single binding either pass (fanout 1) or
				// produce 0/many; we conservatively treat them as
				// pass-through filters here. If they would explode
				// fanout (e.g. a generator), the estimate would
				// underestimate — acceptable for the pre-pass.
				results := ApplyBuiltin(lit.Atom, []binding{walkBinding})
				if len(results) == 0 {
					if lit.Positive {
						alive = false
					}
					continue
				}
				if !lit.Positive {
					alive = false
					continue
				}
				// Adopt the first result for the walk (uniform pick if
				// multiple). Builtins are typically deterministic so
				// |results| = 1 in practice.
				walkBinding = results[rng.Intn(len(results))]
				continue
			}

			rel, hasRel := rels[relKey(lit.Atom.Predicate, len(lit.Atom.Args))]
			if !lit.Positive {
				// Anti-join: succeeds when there are NO matches. No
				// fanout multiplier — anti-joins do not contribute to
				// cardinality growth.
				if hasRel && rel != nil && hasMatch(lit.Atom, rel, walkBinding) {
					alive = false
				}
				continue
			}

			if !hasRel || rel == nil || rel.Len() == 0 {
				alive = false
				continue
			}

			// Resolve match count + a random pick without materialising the
			// full index list when no vars are bound (Cartesian step) — that
			// would allocate O(|rel|) ints inside the inner walk loop and
			// defeat the bounded-cost contract.
			pickIdx, m, ok := pickMatchTuple(lit.Atom, rel, walkBinding, rng)
			if !ok || m == 0 {
				alive = false
				continue
			}
			if !extendWithTuple(walkBinding, lit.Atom, rel.Tuples()[pickIdx]) {
				alive = false
				continue
			}
			fanout *= float64(m)
		}

		if alive {
			sumWeights += float64(seedSize) * fanout
			successful++
		}
	}

	if successful == 0 {
		return 0, false
	}

	estimate := sumWeights / float64(k)
	if estimate < 1 {
		// A single successful walk implies at least one output tuple,
		// regardless of how the sampling probabilities round.
		estimate = 1
	}
	// Saturate at MaxInt32 instead of converting an overflowing
	// float64 to int (whose result is platform-defined and on a
	// signed-int conversion can wrap to a negative value, which
	// would silently make a "huge IDB" look small to the planner —
	// the exact failure mode P2b is meant to fix). Wander-Join can
	// produce arbitrarily large estimates under high fanout
	// compounding; clamping to a sentinel ceiling preserves the
	// "definitely larger than SamplingMaterialiseThreshold" signal
	// the wiring layer cares about.
	const maxEst = float64(1 << 30)
	if estimate > maxEst {
		estimate = maxEst
	}
	return int(estimate + 0.5), true
}

// extendWithTuple binds atom.Args against the values in tup. Returns
// false if a repeated variable in atom.Args resolves to inconsistent
// values (e.g. `R(x, x)` against a tuple where col0 != col1) or if a
// constant arg disagrees with the tuple.
//
// Wildcards are skipped; pre-existing bindings are honoured (the
// walk's binding is shared across steps).
func extendWithTuple(b binding, atom datalog.Atom, tup Tuple) bool {
	for i, arg := range atom.Args {
		if i >= len(tup) {
			return false
		}
		switch a := arg.(type) {
		case datalog.Var:
			if a.Name == "_" {
				continue
			}
			if existing, ok := b[a.Name]; ok {
				eq, err := Compare("=", existing, tup[i])
				if err != nil || !eq {
					return false
				}
				continue
			}
			b[a.Name] = tup[i]
		case datalog.IntConst:
			eq, err := Compare("=", IntVal{V: a.Value}, tup[i])
			if err != nil || !eq {
				return false
			}
		case datalog.StringConst:
			eq, err := Compare("=", StrVal{V: a.Value}, tup[i])
			if err != nil || !eq {
				return false
			}
		case datalog.Wildcard:
			continue
		}
	}
	return true
}

// pickMatchTuple returns (tupleIdx, matchCount, ok) for a positive
// atom step against rel given the current walk binding. matchCount
// is the size of the match set m_i used as the Wander-Join fanout
// multiplier; tupleIdx is one element drawn uniformly at random from
// that set.
//
// Critical: when NO columns are bound (Cartesian step from this
// walk's perspective) we MUST NOT allocate the full match index
// list — that would be O(|rel|) per step per walk and break the
// bounded-cost contract. Instead we sample directly via Intn(|rel|).
//
// Returns ok=false only on a defensive arity-mismatch sanity check;
// real shape errors surface as matchCount=0.
func pickMatchTuple(atom datalog.Atom, rel *Relation, b binding, rng *rand.Rand) (int, int, bool) {
	boundCols := make([]int, 0, len(atom.Args))
	boundVals := make([]Value, 0, len(atom.Args))
	for i, arg := range atom.Args {
		if v, ok := lookupTerm(arg, b); ok {
			boundCols = append(boundCols, i)
			boundVals = append(boundVals, v)
		}
	}
	if len(boundCols) == 0 {
		// No filter columns — every tuple matches. Sample one
		// uniformly at random; m_i = |rel|.
		n := rel.Len()
		if n == 0 {
			return 0, 0, true
		}
		return rng.Intn(n), n, true
	}
	idx := rel.Index(boundCols)
	matches := idx.Lookup(boundVals)
	m := len(matches)
	if m == 0 {
		return 0, 0, true
	}
	return matches[rng.Intn(m)], m, true
}

// hasMatch reports whether at least one tuple in rel matches the
// currently-bound variables of atom. Used by the anti-join branch.
// Avoids the O(|rel|) full-index allocation in the no-bound-cols
// case — anti-join with no bound vars is "is rel non-empty".
func hasMatch(atom datalog.Atom, rel *Relation, b binding) bool {
	boundCols := make([]int, 0, len(atom.Args))
	boundVals := make([]Value, 0, len(atom.Args))
	for i, arg := range atom.Args {
		if v, ok := lookupTerm(arg, b); ok {
			boundCols = append(boundCols, i)
			boundVals = append(boundVals, v)
		}
	}
	if len(boundCols) == 0 {
		return rel.Len() > 0
	}
	idx := rel.Index(boundCols)
	return len(idx.Lookup(boundVals)) > 0
}

// sampleEvalCmp evaluates a comparison literal against the walk's
// current binding. Unbound operands fail-closed (the walk dies) so
// the estimator doesn't double-count.
func sampleEvalCmp(cmp *datalog.Comparison, b binding) bool {
	lv, lok := lookupTerm(cmp.Left, b)
	rv, rok := lookupTerm(cmp.Right, b)
	if !lok || !rok {
		return false
	}
	ok, err := Compare(cmp.Op, lv, rv)
	if err != nil {
		return false
	}
	return ok
}
