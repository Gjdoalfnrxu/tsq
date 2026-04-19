package eval

import (
	"math/rand"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// EstimateRecursiveIDBSizes runs the recursive-IDB cardinality estimator
// (plan.EstimateRecursiveIDB) over every recursive IDB in `prog` and
// writes the resulting hints into `sizeHints` in place. Returns the
// updates applied (for observability — same shape as
// EstimateNonRecursiveIDBSizes).
//
// The base-case cardinality B for each recursive IDB is computed via
// the existing P2b sampler (SampleJoinCardinality) over each base
// rule's plan. When the sampler cannot run on a base rule (e.g. its
// seed relation is empty or absent) that rule contributes 0 to B; if
// every base rule contributes 0 the IDB is sized at SaturatedSizeHint
// (sound-for-ordering — see plan.EstimateRecursiveIDB).
//
// `lookup` is the EDB statistics interface. Pass nil to force every
// recursive IDB to SaturatedSizeHint (default-stats mode per plan
// §3.4). A non-nil lookup whose per-relation entries are missing will
// also degrade to default-stats mode, per relation.
//
// Update semantics match EstimateNonRecursiveIDBSizes: an existing
// sizeHints entry is overwritten only when the computed value is
// strictly larger. Recursive IDBs that the trivial-IDB pre-pass
// already populated (impossible by construction — IdentifyRecursiveIDBs
// excludes trivial heads — but defensive against future overlap) are
// never shrunk.
func EstimateRecursiveIDBSizes(
	prog *datalog.Program,
	baseRels map[string]*Relation,
	sizeHints map[string]int,
	lookup plan.StatsLookup,
) map[string]int {
	if sizeHints == nil {
		sizeHints = map[string]int{}
	}
	updates := map[string]int{}
	if prog == nil {
		return updates
	}

	basePreds := make(map[string]bool, len(baseRels))
	for _, rel := range baseRels {
		if rel == nil {
			continue
		}
		basePreds[rel.Name] = true
	}
	recursives := plan.IdentifyRecursiveIDBs(prog, basePreds)
	if len(recursives) == 0 {
		return updates
	}

	keyed := keyRels(baseRels)
	// Deterministic-by-default rng (matches the trivial-IDB pre-pass).
	rng := rand.New(rand.NewSource(1))

	for _, rec := range recursives {
		baseSize := sampleBaseCardinality(rec, keyed, sizeHints, rng)
		hint64 := plan.EstimateRecursiveIDB(rec, baseSize, lookup)
		// Saturate down to int range using the same ceiling the
		// rest of the pipeline uses.
		var hint int
		if hint64 > int64(plan.SaturatedSizeHint) {
			hint = plan.SaturatedSizeHint
		} else {
			hint = int(hint64)
		}
		if cur, exists := sizeHints[rec.Name]; !exists || hint > cur {
			sizeHints[rec.Name] = hint
		}
		updates[rec.Name] = hint
	}
	return updates
}

// sampleBaseCardinality estimates the union cardinality of every rule
// in idb.BaseRules using SampleJoinCardinality. Rules that fail to
// sample contribute 0 — the union is an upper bound, so dropping a
// rule that we cannot estimate under-counts; that is acceptable here
// because the recursive estimator's behaviour at small B is to
// produce a small geometric estimate, which is still strictly better
// than the default 1000 hint for join ordering. The over-estimation
// safety property (plan §4.5) is provided by the σ-side of the
// estimate (it never under-counts the per-step expansion).
func sampleBaseCardinality(idb plan.RecursiveIDB, rels map[string]*Relation, sizeHints map[string]int, rng *rand.Rand) int64 {
	if !SamplingEnabled || len(idb.BaseRules) == 0 {
		return 0
	}
	var total int64
	for _, rule := range idb.BaseRules {
		planned := plan.SingleRule(rule, sizeHints)
		est, ok := SampleJoinCardinality(planned, rels, SamplingK, rng)
		if !ok {
			continue
		}
		total += int64(est)
		if total > int64(plan.SaturatedSizeHint) {
			return int64(plan.SaturatedSizeHint)
		}
	}
	return total
}

// MakeMaterialisingEstimatorHookWithStats wraps
// MakeMaterialisingEstimatorHook with a follow-up pass that runs the
// recursive-IDB estimator (plan.EstimateRecursiveIDB) using the supplied
// stats lookup. The recursive pass runs AFTER the trivial-IDB pre-pass
// so the recursive estimator sees the trivial heads in sizeHints (not
// that it currently consults them — recursive IDBs depend on base-rel
// stats only — but the ordering avoids any future surprises if the σ
// formula is extended to consult intermediate-IDB sizes).
//
// `lookup` may be nil: the recursive pass then runs in default-stats
// mode and writes SaturatedSizeHint for every recursive IDB. This is
// strictly better than the prior behaviour, where recursive IDBs got
// the default 1000-row hint and the planner happily seeded them.
//
// `materialisedSink` is the same sink the underlying hook owns —
// passed through unchanged.
func MakeMaterialisingEstimatorHookWithStats(
	baseRels map[string]*Relation,
	materialisedSink map[string]*Relation,
	lookup plan.StatsLookup,
) plan.MaterialisingEstimatorHook {
	inner := MakeMaterialisingEstimatorHook(baseRels, materialisedSink)
	return func(prog *datalog.Program, sizeHints map[string]int, maxBindingsPerRule int) map[string]bool {
		extentNames := inner(prog, sizeHints, maxBindingsPerRule)
		_ = EstimateRecursiveIDBSizes(prog, baseRels, sizeHints, lookup)
		return extentNames
	}
}
