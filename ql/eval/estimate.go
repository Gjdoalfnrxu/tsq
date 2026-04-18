package eval

import (
	"context"
	"math/rand"

	"github.com/Gjdoalfnrxu/tsq/ql/datalog"
	"github.com/Gjdoalfnrxu/tsq/ql/plan"
)

// SamplingEnabled toggles the Wander-Join sampling pre-pass inside
// EstimateNonRecursiveIDBSizes. Default is ON (P2b). A package-level
// switch (rather than a per-call argument) keeps the EstimatorHook
// signature stable across the planner boundary; callers that want
// strict materialised-only semantics for a benchmark or regression
// test set this to false at process start.
//
// The toggle is consulted at the top of each pre-pass invocation; it
// is not goroutine-safe to flip it concurrently with an in-flight
// pre-pass, but flipping once at process init is safe.
var SamplingEnabled = true

// SamplingMaterialiseThreshold is the upper bound on a sampled
// cardinality estimate at which the pre-pass will still proceed to
// materialise the IDB. Above this threshold we record the sampled
// hint and skip materialisation entirely — the load-bearing P2b win
// for `_disj_2`-shape IDBs that would otherwise OOM in the pre-pass.
//
// Concretely: an IDB whose true size is ~500k and whose body is a
// chain of large EDB joins blows ~5GB through the existing
// materialising path even with the binding cap engaged (the cap fires
// AFTER intermediate bindings are allocated; the sampled estimate
// fires BEFORE any binding allocation). Choosing 50k as the
// threshold means anything plausibly small enough to be a "tiny seed"
// or to participate in cheap downstream materialisation is still
// materialised, while genuinely large IDBs are estimated and skipped.
const SamplingMaterialiseThreshold = 50000

// SamplingK is the per-rule sample budget for the pre-pass. K=1024
// matches DefaultSampleK; exposed separately so a benchmark or test
// can tighten it without touching the algorithm constant.
var SamplingK = DefaultSampleK

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
	// head with the same name+arity. The exclusion matters because the
	// pipeline has the arity-shadow case where a name (e.g. `LocalFlow`)
	// is both a schema relation AND the head of system-injected rules
	// that populate it from other facts. A class extent body that
	// touches a same-arity shadowed name would be materialised against
	// the EMPTY base copy before the IDB rules run, then stripped from
	// the program — leaving the extent permanently empty.
	//
	// Crucially, this map is arity-keyed (relKey), not name-keyed. The
	// production CodeQL pattern `class Symbol extends @symbol { Symbol()
	// { Symbol(this,_,_,_) } }` emits a head `Symbol/1` whose body
	// references base `Symbol/4` — same name, different arity. A
	// name-only key would shadow `Symbol/4` and silently exclude every
	// real bridge fixture (`TaintSink`, `TaintSource`, `Sanitizer`,
	// `TaintedSym`, `TaintedField`) from materialisation. Arity-keyed
	// shadowing only fires for the genuine same-name+same-arity IDB
	// case (e.g. `LocalFlow/3` head shadowing `LocalFlow/3` base).
	headPreds := make(map[string]bool, len(prog.Rules))
	for _, rule := range prog.Rules {
		headPreds[relKey(rule.Head.Predicate, len(rule.Head.Args))] = true
	}
	basePreds := make(map[string]bool, len(baseRels))
	for _, rel := range baseRels {
		if rel == nil {
			continue
		}
		if headPreds[relKey(rel.Name, rel.Arity)] {
			// Schema name+arity that an IDB rule also produces — genuine
			// arity-shadow case. Skip for materialisation safety.
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
// returned hook. A nil sink panics at construction time: silently
// dropping the materialised relations would let the planner strip the
// extent rules from the program (it sees the head names in the
// returned set) without making the relations available to Evaluate,
// leaving the extents permanently empty at query time. Callers that
// don't care about materialisation can use MakeEstimatorHook instead.
func MakeMaterialisingEstimatorHook(
	baseRels map[string]*Relation,
	materialisedSink map[string]*Relation,
) plan.MaterialisingEstimatorHook {
	if materialisedSink == nil {
		panic("eval.MakeMaterialisingEstimatorHook: materialisedSink must be non-nil; use MakeEstimatorHook for non-materialising callers")
	}
	return func(prog *datalog.Program, sizeHints map[string]int, maxBindingsPerRule int) map[string]bool {
		mats, _ := MaterialiseClassExtents(prog, baseRels, sizeHints, maxBindingsPerRule)
		extentNames := make(map[string]bool, len(mats))
		for k, rel := range mats {
			materialisedSink[k] = rel
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
		// Both MaterialiseClassExtents and EstimateNonRecursiveIDBSizes
		// mutate sizeHints in place; we don't need their return values
		// for the planner contract.
		_ = EstimateNonRecursiveIDBSizes(prog, merged, sizeHints, maxBindingsPerRule)
		return extentNames
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

	// Deterministic-by-default sampling rng for the whole pre-pass: a
	// fresh fixed-seed source so two pre-pass invocations on the same
	// program produce the same hint values (the planner's ordering
	// rules are deterministic, so the planner output stays
	// deterministic too). Per-rule walks share this rng — that's fine,
	// they don't interleave (the loop is serial).
	sampleRng := rand.New(rand.NewSource(1))

	for _, t := range trivials {
		// P2b — Wander-Join sampling pre-pass.
		//
		// Strategy: try sampling first. If the sampled estimate is
		// above SamplingMaterialiseThreshold we record the sampled
		// hint and SKIP materialisation entirely — this is the load-
		// bearing OOM avoidance, since the materialising path can
		// allocate gigabytes of intermediate bindings even with the
		// per-rule cap engaged. If the sampled estimate is small (or
		// sampling cannot run on this rule shape), fall through to
		// the existing materialising path so downstream trivial rules
		// that reference this IDB can still resolve their own
		// sample/materialise decisions against a real relation.
		if SamplingEnabled {
			sampledOK := false
			sampled := 0
			for _, rule := range t.Rules {
				planned := plan.SingleRule(rule, sizeHints)
				est, ok := SampleJoinCardinality(planned, keyed, SamplingK, sampleRng)
				if !ok {
					sampledOK = false
					break
				}
				// Multiple rules with the same head are unioned in
				// the materialising path; sampled cardinalities
				// upper-bound the union sum (no dedup info from
				// sampling, but a sum is still an unbiased upper
				// bound for planner scoring). For the OOM-avoidance
				// contract we err on the side of overestimating.
				sampled += est
				sampledOK = true
			}
			if sampledOK && sampled > SamplingMaterialiseThreshold {
				if cur, exists := sizeHints[t.Name]; !exists || sampled > cur {
					sizeHints[t.Name] = sampled
				}
				updates[t.Name] = sampled
				// Skip materialisation: downstream rules that
				// reference this IDB will themselves fail to sample
				// (no extent to draw from) and fall back to the
				// materialising path. That path will then hit the
				// binding cap on this very IDB body and fail-soft
				// per the existing best-effort contract — exactly
				// the behaviour we want, since by hypothesis the
				// IDB is too large to materialise.
				continue
			}
			// If sampling produced a small estimate, fall through to
			// materialise normally; no extra cost since the
			// materialising path would have run anyway.
		}

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
