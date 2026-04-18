package eval

import (
	"context"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// EstimateNonRecursiveIDBSizes pre-computes the cardinality of every
// "trivially evaluable" derived predicate in prog (see plan.IdentifyTrivialIDBs)
// using the supplied base relations, and writes each computed count into
// sizeHints (in place). The returned map mirrors the writes, so callers that
// want a separate copy can use it.
//
// Why this exists: the planner's greedy join ordering uses sizeHints to score
// literals. Without entries for derived predicates, IDB literals fall through
// to defaultSizeHint=1000 (ql/plan/join.go). For queries whose ideal seed is
// a tiny derived predicate (e.g. isUseStateSetterCall, 7 tuples in the React
// bridge), the planner picks a Cartesian-heavy order based on the
// 1000-tuple lie and OOMs (issue #88). The previous fix re-planned BETWEEN
// strata, but co-stratified rules — where the seed and the explody rule are
// in the same SCC — never benefit because the seed is not yet materialised
// when its sibling rule is planned. This pre-pass is the missing piece.
//
// Restrictions on what is "trivial": rules with only positive base-predicate
// atoms and comparisons — no negation, no aggregates, no recursion, no
// references to other IDBs. Predicates not meeting this bar are left at the
// default hint and will benefit from the existing between-strata refresh
// only if they happen to be in a later stratum.
//
// Update semantics: an existing sizeHints entry is overwritten only when the
// computed value is strictly larger (defensive — a manually-supplied small
// base count for a name colliding with an IDB head must not be silently
// shrunk; see the parallel rule in seminaive.go's between-strata refresh).
//
// Errors during evaluation of an individual trivial IDB (e.g. binding cap)
// are silently absorbed — the pre-pass is best-effort. If a "trivial" rule
// itself OOMs we'd rather degrade to the default hint than fail compilation.
//
// maxBindingsPerRule is the per-rule binding cap applied to each trivial IDB
// evaluation. Without it the pre-pass can fully materialise an N×M join just
// to count head facts and OOM (issue #130 — mastodon corpus blew 30 GB RSS
// inside this function on setStateUpdaterCallsFn). Pass 0 to disable the cap
// (legacy behaviour, not recommended on real corpora). When the cap fires the
// failed/break branch below treats the IDB as "could not estimate" and the
// default hint applies — exactly the right semantics for a best-effort pass.
//
// Returns the slice of (name, computed-size) updates actually applied, for
// observability/testing.
// MakeEstimatorHook returns a plan.EstimatorHook closed over the supplied
// base relations. The returned hook delegates to EstimateNonRecursiveIDBSizes,
// matching its best-effort / cap-honouring semantics. This is the bridge that
// lets plan.EstimateAndPlan call into the eval-side trivial-IDB pre-pass
// without plan importing eval (eval already imports plan).
//
// Use it from cmd/tsq's compileAndEval (and any other call site that wants
// the single-pass estimate-then-plan flow) instead of the previous two-pass
// "plan → EstimateNonRecursiveIDBSizes → RePlanStratum/RePlanQuery"
// ceremony. The binding cap from PR #132 (issue #130) is preserved end-to-end
// because it is a parameter of the hook signature, not a closed-over constant.
func MakeEstimatorHook(baseRels map[string]*Relation) plan.EstimatorHook {
	return func(prog *datalog.Program, sizeHints map[string]int, maxBindingsPerRule int) map[string]int {
		return EstimateNonRecursiveIDBSizes(prog, baseRels, sizeHints, maxBindingsPerRule)
	}
}

// MaterialiseClassExtents evaluates every rule in prog whose ClassExtent
// flag is true AND whose body matches plan.IsClassExtentBody, returning
// the materialised relations keyed by relKey() and the set of head names
// that were materialised (for handing back to the planner). A rule that
// fails the structural body check (e.g. a class with an expensive
// CharPred over multiple large extents) is left for normal evaluation.
//
// Multiple rules sharing the same head are unioned into a single
// relation — this is how concrete-class dispatch and abstract-class
// subclass-union both end up as one extent per class name.
//
// Materialisation is iterative: an extent whose body references another
// already-materialised extent becomes eligible on the next pass. This
// supports shallow class chains (e.g. an abstract class whose body
// unions concrete subclass extents that are themselves materialised).
// Convergence is bounded by the rule count.
//
// Errors are absorbed silently per the EstimatorHook contract: a
// materialisation that hits the binding cap is skipped (its rules are
// left for the main Evaluate path to handle). The cap exists to bound
// pathological char-preds; see EstimateNonRecursiveIDBSizes for the
// same reasoning at the trivial-IDB layer.
//
// The size of each materialised extent is also written into sizeHints
// (only-grow), so the join planner gets accurate cardinalities for
// extent literals downstream.
func MaterialiseClassExtents(
	prog *datalog.Program,
	baseRels map[string]*Relation,
	sizeHints map[string]int,
	maxBindingsPerRule int,
) (materialised map[string]*Relation, sizeUpdates map[string]int) {
	materialised = map[string]*Relation{}
	sizeUpdates = map[string]int{}
	if prog == nil {
		return
	}
	if sizeHints == nil {
		sizeHints = map[string]int{}
	}

	// "Truly base" = registered base relation AND no IDB rule defines a
	// head with the same name. The exclusion matters because the pipeline
	// has the arity-shadow case where a name (e.g. `LocalFlow`) is both a
	// schema relation AND the head of system-injected rules that
	// populate it from other facts (relkey disambiguates by arity at
	// eval time, but for materialisation we need to know "is this thing
	// fully populated already?"). A class extent body that touches a
	// shadowed name would be materialised against the EMPTY base copy
	// before the IDB rules run, then stripped from the program — leaving
	// the extent permanently empty. We avoid that by treating
	// schema-registered names with IDB rules as not-yet-trustworthy.
	headPreds := make(map[string]bool, len(prog.Rules))
	for _, rule := range prog.Rules {
		headPreds[rule.Head.Predicate] = true
	}
	basePreds := make(map[string]bool, len(baseRels))
	for _, rel := range baseRels {
		if rel == nil {
			continue
		}
		if headPreds[rel.Name] {
			// Schema name that an IDB rule also produces — arity-shadow
			// case. Skip for materialisation safety.
			continue
		}
		basePreds[rel.Name] = true
	}

	// Group ClassExtent-tagged rules by head name. Skip rules whose head
	// arity is not 1 — class extents are arity-1 by construction; a
	// tagged arity-N rule would be a desugarer bug, but we'd rather
	// silently skip than crash.
	rulesByHead := map[string][]datalog.Rule{}
	headOrder := []string{}
	for _, rule := range prog.Rules {
		if !rule.ClassExtent {
			continue
		}
		if len(rule.Head.Args) != 1 {
			continue
		}
		name := rule.Head.Predicate
		if _, seen := rulesByHead[name]; !seen {
			headOrder = append(headOrder, name)
		}
		rulesByHead[name] = append(rulesByHead[name], rule)
	}

	// Snapshot the materialisation set as a name-only map so
	// IsClassExtentBody can consult it. Updated each pass as new extents
	// are materialised.
	matNames := map[string]bool{}
	keyed := keyRels(baseRels)

	for {
		progress := false
		for _, name := range headOrder {
			if matNames[name] {
				continue
			}
			rules := rulesByHead[name]
			// All rules defining this extent must match the structural
			// shape — if any rule is too complex, fall back to normal
			// evaluation for the whole head (otherwise we'd have a
			// half-materialised extent, which is worse).
			eligible := true
			for _, rule := range rules {
				if !plan.IsClassExtentBody(rule.Body, basePreds, matNames) {
					eligible = false
					break
				}
			}
			if !eligible {
				continue
			}

			// Evaluate every rule and union into one head relation.
			// Arity is fixed to 1 by the filter above.
			head := NewRelation(name, 1)
			failed := false
			for _, rule := range rules {
				planned := plan.SingleRule(rule, sizeHints)
				tuples, err := Rule(context.Background(), planned, keyed, maxBindingsPerRule)
				if err != nil {
					failed = true
					break
				}
				for _, tup := range tuples {
					head.Add(tup)
				}
			}
			if failed {
				continue
			}

			materialised[relKey(name, 1)] = head
			matNames[name] = true
			keyed[relKey(name, 1)] = head
			n := head.Len()
			if cur, exists := sizeHints[name]; !exists || n > cur {
				sizeHints[name] = n
			}
			sizeUpdates[name] = n
			progress = true
		}
		if !progress {
			break
		}
	}
	return
}

// MakeMaterialisingEstimatorHook returns a plan.MaterialisingEstimatorHook
// that:
//  1. Materialises every eligible class extent (per MaterialiseClassExtents).
//  2. Stashes the resulting *Relation values in the supplied sink map so
//     the caller can hand them to Evaluate via WithMaterialisedClassExtents.
//  3. Runs the existing trivial-IDB pre-pass so non-extent IDBs still
//     get cardinality hints.
//
// Splitting the *Relation transport into a sink map (rather than
// extending the hook return type) keeps plan.MaterialisingEstimatorHook
// from depending on eval.Relation — preserving the no-import-cycle
// invariant. The caller owns the sink map and passes it to both
// MakeMaterialisingEstimatorHook and eval.WithMaterialisedClassExtents.
//
// `materialisedSink` MUST be non-nil and is mutated in place by the
// returned hook. Callers that don't care about materialisation can use
// MakeEstimatorHook instead.
func MakeMaterialisingEstimatorHook(
	baseRels map[string]*Relation,
	materialisedSink map[string]*Relation,
) plan.MaterialisingEstimatorHook {
	return func(prog *datalog.Program, sizeHints map[string]int, maxBindingsPerRule int) (map[string]int, map[string]bool) {
		mats, updates := MaterialiseClassExtents(prog, baseRels, sizeHints, maxBindingsPerRule)
		extentNames := make(map[string]bool, len(mats))
		for k, rel := range mats {
			if materialisedSink != nil {
				materialisedSink[k] = rel
			}
			if rel != nil {
				extentNames[rel.Name] = true
			}
		}
		// Run the trivial-IDB pre-pass too so non-extent trivials get
		// cardinality hints. It will see the materialised extents as
		// base-like via baseRels — but baseRels is a closed-over input,
		// so we have to fold them in here.
		merged := make(map[string]*Relation, len(baseRels)+len(mats))
		for k, v := range baseRels {
			merged[k] = v
		}
		for k, v := range mats {
			// keyRels-style merge: the keys are already relKey'd because
			// MaterialiseClassExtents stashes by relKey().
			merged[k] = v
		}
		trivUpdates := EstimateNonRecursiveIDBSizes(prog, merged, sizeHints, maxBindingsPerRule)
		// Combine update views. Materialisation updates win on conflict
		// (they are exact, not pre-pass estimates).
		out := make(map[string]int, len(updates)+len(trivUpdates))
		for k, v := range trivUpdates {
			out[k] = v
		}
		for k, v := range updates {
			out[k] = v
		}
		return out, extentNames
	}
}

func EstimateNonRecursiveIDBSizes(prog *datalog.Program, baseRels map[string]*Relation, sizeHints map[string]int, maxBindingsPerRule int) map[string]int {
	if sizeHints == nil {
		sizeHints = map[string]int{}
	}
	if prog == nil {
		return map[string]int{}
	}

	basePreds := make(map[string]bool, len(baseRels))
	for _, rel := range baseRels {
		if rel == nil {
			continue
		}
		basePreds[rel.Name] = true
	}

	// trivials is in topological order: each entry depends only on base
	// predicates and on earlier entries. We materialise as we go and stash
	// each result back into `keyed` so subsequent rules can reference it.
	trivials := plan.IdentifyTrivialIDBs(prog, basePreds)
	updates := make(map[string]int, len(trivials))

	keyed := keyRels(baseRels)

	for _, t := range trivials {
		head := NewRelation(t.Name, t.Arity)
		failed := false
		for _, rule := range t.Rules {
			planned := plan.SingleRule(rule, sizeHints)
			// Apply the user-supplied binding cap so a pathological body
			// (cross-product before a selective join) cannot eat all RAM
			// here. On cap-exceeded the err branch below treats the IDB as
			// unestimatable and falls through to the default hint. Issue
			// #130: passing 0 here meant pre-pass evaluation was unbounded
			// and OOMed on real corpora before the cap could ever fire on
			// the main eval pass.
			tuples, err := Rule(context.Background(), planned, keyed, maxBindingsPerRule)
			if err != nil {
				// Best-effort: skip this IDB entirely on any error so we
				// don't half-populate hints. The default hint will apply
				// and the between-strata refresh in Evaluate will catch up
				// (for non-co-stratified cases at least).
				failed = true
				break
			}
			for _, tup := range tuples {
				head.Add(tup)
			}
		}
		if failed {
			continue
		}
		// Make this IDB visible to subsequent trivial rules that reference
		// it. keyRels uses (name,arity) so this won't shadow a base relation
		// of a different arity. Topological order guarantees no later
		// trivial depends on something not yet in `keyed`.
		keyed[relKey(t.Name, t.Arity)] = head

		n := head.Len()
		if cur, exists := sizeHints[t.Name]; !exists || n > cur {
			sizeHints[t.Name] = n
		}
		updates[t.Name] = n
	}

	return updates
}
